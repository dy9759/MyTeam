package agent

import (
	"context"

	"github.com/MyAIOSHub/MyTeam/server/pkg/llmclient"
)

// CloudBackend implements Backend using an LLM API (DashScope, OpenAI-compatible, etc.).
type CloudBackend struct {
	LLM *llmclient.Client
}

// Execute runs a prompt via the LLM chat API and returns a Session for streaming results.
func (b *CloudBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	msgChan := make(chan Message, 10)
	resChan := make(chan Result, 1)

	session := &Session{
		Messages: msgChan,
		Result:   resChan,
	}

	go func() {
		defer close(msgChan)
		defer close(resChan)

		msgChan <- Message{Type: MessageStatus, Status: "Processing..."}

		system := opts.SystemPrompt
		if system == "" {
			system = "You are a helpful AI assistant working on a software project. Respond concisely and helpfully."
		}

		text, err := b.LLM.Chat(ctx, system, []llmclient.Message{
			{Role: "user", Content: prompt},
		})
		if err != nil {
			resChan <- Result{Status: "failed", Error: err.Error()}
			return
		}

		msgChan <- Message{Type: MessageText, Content: text}
		resChan <- Result{Status: "completed", Output: text}
	}()

	return session, nil
}
