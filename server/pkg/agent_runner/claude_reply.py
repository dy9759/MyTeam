"""
MyTeam personal agent reply runner.

Invocation: python3 claude_reply.py
  stdin  = user prompt (UTF-8 text)
  env    = ANTHROPIC_BASE_URL / ANTHROPIC_API_KEY / ANTHROPIC_MODEL
           AGENT_SYSTEM_PROMPT (optional)
  stdout = NDJSON events; final line = {"type":"done","text":"..."}
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
        from claude_agent_sdk import query, ClaudeAgentOptions
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
    base_url = os.environ.get("ANTHROPIC_BASE_URL", "")
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    model = os.environ.get("ANTHROPIC_MODEL") or os.environ.get("OPENAI_MODEL") or "claude-sonnet-4-20250514"

    options = ClaudeAgentOptions(
        model=model,
        system_prompt=system_prompt,
        max_turns=1,
        env={
            "ANTHROPIC_BASE_URL": base_url,
            "ANTHROPIC_API_KEY": api_key,
        },
    )

    try:
        reply = ""
        async for message in query(prompt=prompt, options=options):
            # Extract text from message content blocks
            if hasattr(message, "content"):
                for block in message.content if isinstance(message.content, list) else [message.content]:
                    if hasattr(block, "text"):
                        reply += block.text
                    elif isinstance(block, str):
                        reply += block
            elif hasattr(message, "result") and hasattr(message.result, "text"):
                reply += message.result.text

        if not reply:
            emit({"type": "error", "message": "agent returned empty reply"})
            return 1

        emit({"type": "done", "text": reply})
        return 0
    except Exception as e:  # noqa: BLE001
        emit({"type": "error", "message": str(e)})
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
