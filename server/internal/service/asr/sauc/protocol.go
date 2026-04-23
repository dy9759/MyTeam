// Package sauc implements the binary framing protocol used by Volcengine
// streaming ASR (大模型流式语音识别, aka sauc bigmodel). Shared with the
// 豆包语音妙记 lark stream family which rides the same protocol.
//
// Frame layout (all integers big-endian):
//
//	 Byte 0: protocol_version(4b)=0x1 | header_size(4b)=0x1   → 0x11
//	 Byte 1: message_type(4b)       | message_type_specific_flags(4b)
//	 Byte 2: serialization_method(4b) | compression_method(4b)
//	 Byte 3: reserved
//	 [Byte 4..7]: sequence (int32 BE) — when flags bit 0 is set
//	 [Byte ..]: payload_size (uint32 BE)
//	 [Byte ..]: payload (optionally gzipped)
//
// Reference: https://www.volcengine.com/docs/6561/1354869
package sauc

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the only supported upstream version today.
const ProtocolVersion byte = 0x1

// HeaderSize is expressed in 32-bit words. SDK only emits 4-byte headers.
const HeaderSize byte = 0x1

// MessageType high-nibble values.
const (
	MsgFullClientRequest  byte = 0b0001
	MsgAudioOnlyRequest   byte = 0b0010
	MsgFullServerResponse byte = 0b1001
	MsgServerError        byte = 0b1111
)

// MessageTypeSpecificFlag low-nibble values (same namespace for client +
// server; server reuses 0b0011 to signal the last response packet).
const (
	FlagNoSequence      byte = 0b0000
	FlagPosSequence     byte = 0b0001
	FlagNegSequence     byte = 0b0010
	FlagNegWithSequence byte = 0b0011
)

// SerializationMethod values.
const (
	SerialRaw  byte = 0b0000
	SerialJSON byte = 0b0001
)

// CompressionMethod values.
const (
	CompressNone byte = 0b0000
	CompressGzip byte = 0b0001
)

// Frame is the decoded representation of a single upstream or downstream
// protocol message. Payload is already decompressed and deserialized into
// bytes; callers handle JSON parsing themselves so we avoid forcing a
// schema on the generic relay path.
type Frame struct {
	MessageType byte
	Flags       byte
	Serial      byte
	Compression byte
	// Sequence is only meaningful when Flags & FlagPosSequence (or
	// NegWithSequence) is set. Zero otherwise.
	Sequence int32
	// ErrorCode is only set for MsgServerError frames.
	ErrorCode uint32
	// Payload is the decompressed bytes. For JSON frames the caller
	// json.Unmarshal; for audio frames it's raw PCM.
	Payload []byte
}

// IsLast reports whether the frame carries the last-packet sentinel.
// Server uses FlagNegWithSequence on the terminal response.
func (f Frame) IsLast() bool {
	return f.Flags == FlagNegSequence || f.Flags == FlagNegWithSequence
}

// buildHeader packs the 4-byte header.
func buildHeader(msgType, flags, serial, compression byte) [4]byte {
	return [4]byte{
		(ProtocolVersion << 4) | HeaderSize,
		(msgType << 4) | (flags & 0x0F),
		(serial << 4) | (compression & 0x0F),
		0x00,
	}
}

// EncodeFullClientRequest builds the first upstream frame. JSON config is
// gzip-compressed, flagged as POS_SEQUENCE with sequence=1 per the V3
// bigmodel handshake.
func EncodeFullClientRequest(configJSON []byte, sequence int32) ([]byte, error) {
	payload, err := gzipBytes(configJSON)
	if err != nil {
		return nil, fmt.Errorf("gzip config: %w", err)
	}
	return encodeFramed(
		MsgFullClientRequest,
		FlagPosSequence,
		SerialJSON,
		CompressGzip,
		sequence,
		payload,
	), nil
}

// EncodeAudioFrame packs a PCM chunk. When last is true the sequence is
// inverted to negative and the last-packet flag is raised.
func EncodeAudioFrame(pcm []byte, sequence int32, last bool) ([]byte, error) {
	payload, err := gzipBytes(pcm)
	if err != nil {
		return nil, fmt.Errorf("gzip audio: %w", err)
	}
	flags := FlagPosSequence
	seq := sequence
	if last {
		flags = FlagNegWithSequence
		seq = -sequence
	}
	return encodeFramed(
		MsgAudioOnlyRequest,
		flags,
		SerialRaw,
		CompressGzip,
		seq,
		payload,
	), nil
}

// encodeFramed writes [header | sequence(4) | size(4) | payload].
func encodeFramed(msgType, flags, serial, compression byte, sequence int32, payload []byte) []byte {
	h := buildHeader(msgType, flags, serial, compression)
	out := make([]byte, 0, 4+4+4+len(payload))
	out = append(out, h[:]...)
	var seqBuf [4]byte
	binary.BigEndian.PutUint32(seqBuf[:], uint32(sequence))
	out = append(out, seqBuf[:]...)
	var sizeBuf [4]byte
	binary.BigEndian.PutUint32(sizeBuf[:], uint32(len(payload)))
	out = append(out, sizeBuf[:]...)
	out = append(out, payload...)
	return out
}

// Decode parses a server-sent frame. Server frames always include a
// sequence (response) or an error-code prefix; both formats carry a
// uint32 payload-size before the payload.
func Decode(raw []byte) (Frame, error) {
	if len(raw) < 4 {
		return Frame{}, errors.New("sauc: frame shorter than header")
	}
	headerSize := int(raw[0]&0x0F) * 4
	if headerSize < 4 || len(raw) < headerSize {
		return Frame{}, fmt.Errorf("sauc: bad header size %d", headerSize)
	}
	f := Frame{
		MessageType: raw[1] >> 4,
		Flags:       raw[1] & 0x0F,
		Serial:      raw[2] >> 4,
		Compression: raw[2] & 0x0F,
	}
	rest := raw[headerSize:]

	switch f.MessageType {
	case MsgFullServerResponse:
		if len(rest) < 8 {
			return Frame{}, errors.New("sauc: server response too short")
		}
		f.Sequence = int32(binary.BigEndian.Uint32(rest[:4]))
		size := binary.BigEndian.Uint32(rest[4:8])
		if uint32(len(rest)-8) < size {
			return Frame{}, fmt.Errorf("sauc: short payload want=%d got=%d", size, len(rest)-8)
		}
		f.Payload = rest[8 : 8+size]
	case MsgServerError:
		if len(rest) < 8 {
			return Frame{}, errors.New("sauc: error frame too short")
		}
		f.ErrorCode = binary.BigEndian.Uint32(rest[:4])
		size := binary.BigEndian.Uint32(rest[4:8])
		if uint32(len(rest)-8) < size {
			return Frame{}, fmt.Errorf("sauc: short error payload want=%d got=%d", size, len(rest)-8)
		}
		f.Payload = rest[8 : 8+size]
	default:
		// Unknown type: expose what we have, let caller decide.
		if len(rest) >= 4 {
			size := binary.BigEndian.Uint32(rest[:4])
			if uint32(len(rest)-4) >= size {
				f.Payload = rest[4 : 4+size]
			}
		}
	}

	if f.Compression == CompressGzip && len(f.Payload) > 0 {
		decoded, err := gunzipBytes(f.Payload)
		if err != nil {
			return Frame{}, fmt.Errorf("gunzip payload: %w", err)
		}
		f.Payload = decoded
	}
	return f, nil
}

// gzipBytes compresses src with default compression level.
func gzipBytes(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(src); err != nil {
		_ = gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gunzipBytes decompresses a gzip payload.
func gunzipBytes(src []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		return nil, err
	}
	return out, nil
}
