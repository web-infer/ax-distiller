#!/usr/bin/env python3
"""
Runs browser-use against each prompt in prompts.json.
Writes results to results/bu_results.jsonl.

Same model as ax-distiller: claude-haiku-4-5-20251001
Override: BU_MODEL env var
"""

# # ax-distiller (all 5 prompts)
# ANTHROPIC_API_KEY=<key> bash benchmark/run_ax.sh

# # browser-use (same model, same prompts)
# ANTHROPIC_API_KEY=<key> python3 benchmark/run_bu.py

# # compare results
# python3 benchmark/compare.py

import asyncio
import json
import os
import sys
import time
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent
PROMPTS_FILE = SCRIPT_DIR / "prompts.json"
OUT_FILE = SCRIPT_DIR / "results" / "bu_results.jsonl"
MAX_STEPS = int(os.getenv("MAX_STEPS", "10"))
MODEL = os.getenv("BU_MODEL", "claude-haiku-4-5-20251001")


def pick_llm():
    if not os.getenv("ANTHROPIC_API_KEY"):
        print("Error: ANTHROPIC_API_KEY not set", file=sys.stderr)
        sys.exit(1)
    from browser_use.llm import ChatAnthropic
    return ChatAnthropic(model=MODEL)


async def run_single(prompt: str) -> dict:
    from browser_use import Agent

    llm = pick_llm()
    turns_ref = [0]

    async def on_step(browser_state, agent_output, step_num):
        turns_ref[0] = step_num

    agent = Agent(task=prompt, llm=llm, register_new_step_callback=on_step)

    start = time.monotonic()
    try:
        history = await agent.run(max_steps=MAX_STEPS)
        result = history.final_result() or ""
    except Exception as e:
        result = f"ERROR: {e}"
    wall_ms = int((time.monotonic() - start) * 1000)

    svc = agent.token_cost_service
    input_tok = sum(e.usage.prompt_tokens for e in svc.usage_history)
    output_tok = sum(e.usage.completion_tokens for e in svc.usage_history)

    return {
        "tool": "browser-use",
        "model": MODEL,
        "input_tokens": input_tok,
        "output_tokens": output_tok,
        "total_tokens": input_tok + output_tok,
        "wall_ms": wall_ms,
        "turns": turns_ref[0],
        "result": result[:500],
    }


async def main():
    prompts = json.loads(PROMPTS_FILE.read_text())
    OUT_FILE.parent.mkdir(parents=True, exist_ok=True)

    with open(OUT_FILE, "w") as f:
        for p in prompts:
            print(f"=== [{p['id']}] {p['prompt']} ===")
            result = await run_single(p["prompt"])
            result["id"] = p["id"]
            result["prompt"] = p["prompt"]
            f.write(json.dumps(result) + "\n")
            f.flush()
            print(f"  tokens={result['total_tokens']}  time={result['wall_ms']}ms  turns={result['turns']}")
            print(f"  result: {result['result'][:120]}")
            print()

    print(f"Results written to {OUT_FILE}")


if __name__ == "__main__":
    asyncio.run(main())
