"""
MyTeam personal agent reply runner.

Invocation: python3 claude_reply.py
  stdin  = user prompt (UTF-8 text)
  env    = OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL
           (or ANTHROPIC_BASE_URL / ANTHROPIC_API_KEY / ANTHROPIC_MODEL)
           AGENT_SYSTEM_PROMPT (optional)
  stdout = NDJSON events; final line = {"type":"done","text":"..."}
           intermediate {"type":"status"} / {"type":"error"} allowed
  exit 0 = success; non-zero = failure
"""
import asyncio
import json
import os
import sys


def emit(event: dict) -> None:
    print(json.dumps(event, ensure_ascii=False), flush=True)


async def main() -> int:
    try:
        from claude_agent_sdk import ClaudeAgentOptions, ClaudeSDKClient
    except ImportError:
        emit({"type": "error", "message": "claude-agent-sdk not installed"})
        return 2

    prompt = sys.stdin.read().strip()
    if not prompt:
        emit({"type": "error", "message": "empty prompt"})
        return 3

    system_prompt = os.environ.get(
        "AGENT_SYSTEM_PROMPT",
        "You are a helpful AI assistant on the MyTeam platform. Reply concisely.",
    )
    model = (
        os.environ.get("ANTHROPIC_MODEL")
        or os.environ.get("OPENAI_MODEL")
        or "sonnet"
    )

    options = ClaudeAgentOptions(
        system_prompt=system_prompt,
        max_turns=1,
        model=model,
    )

    try:
        reply = ""
        async with ClaudeSDKClient(options=options) as client:
            await client.query(prompt=prompt)
            async for message in client.receive_response():
                if hasattr(message, "content"):
                    for block in message.content:
                        if hasattr(block, "text"):
                            reply += block.text
        emit({"type": "done", "text": reply})
        return 0
    except Exception as e:  # noqa: BLE001
        emit({"type": "error", "message": str(e)})
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
