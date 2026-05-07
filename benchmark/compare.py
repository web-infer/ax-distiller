#!/usr/bin/env python3

import json
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent
AX_FILE = SCRIPT_DIR / "results" / "ax_results.jsonl"
BU_FILE = SCRIPT_DIR / "results" / "bu_results.jsonl"


def load(path: Path) -> dict:
    results = {}
    if not path.exists():
        return results
    for line in path.read_text().splitlines():
        if line.strip():
            r = json.loads(line)
            results[r["id"]] = r
    return results


def fmt_tokens(n: int) -> str:
    return f"{n:,}" if n else "—"

def fmt_time(ms: int) -> str:
    return f"{ms/1000:.1f}s" if ms else "—"


def main():
    ax = load(AX_FILE)
    bu = load(BU_FILE)
    ids = list(dict.fromkeys(list(ax.keys()) + list(bu.keys())))

    if not ids:
        print("No results found. Run run_ax.sh and/or run_bu.py first.")
        return

    col = 18
    header = f"{'Prompt ID':<20} {'Metric':<16} {'ax-distiller':>{col}} {'browser-use':>{col}}"
    print(header)
    print("─" * len(header))

    ax_totals = {"input": 0, "output": 0, "total": 0, "wall_ms": 0, "n": 0}
    bu_totals = {"input": 0, "output": 0, "total": 0, "wall_ms": 0, "n": 0}

    for pid in ids:
        a = ax.get(pid, {})
        b = bu.get(pid, {})

        rows = [
            ("input tokens",  fmt_tokens(a.get("input_tokens", 0)),  fmt_tokens(b.get("input_tokens", 0))),
            ("output tokens", fmt_tokens(a.get("output_tokens", 0)), fmt_tokens(b.get("output_tokens", 0))),
            ("total tokens",  fmt_tokens(a.get("total_tokens", 0)),  fmt_tokens(b.get("total_tokens", 0))),
            ("wall time",     fmt_time(a.get("wall_ms", 0)),         fmt_time(b.get("wall_ms", 0))),
            ("turns",         str(a.get("turns", "—")),              str(b.get("turns", "—"))),
        ]

        first = True
        for metric, av, bv in rows:
            label = pid if first else ""
            print(f"{label:<20} {metric:<16} {av:>{col}} {bv:>{col}}")
            first = False

        ar = a.get("result", "")[:80] if a else "—"
        br = b.get("result", "")[:80] if b else "—"
        print(f"{'':20} {'ax result':<16} {ar}")
        print(f"{'':20} {'bu result':<16} {br}")
        print()

        for src, data in [("ax", a), ("bu", b)]:
            totals = ax_totals if src == "ax" else bu_totals
            if data:
                totals["input"]   += data.get("input_tokens", 0)
                totals["output"]  += data.get("output_tokens", 0)
                totals["total"]   += data.get("total_tokens", 0)
                totals["wall_ms"] += data.get("wall_ms", 0)
                totals["n"]       += 1

    print("─" * len(header))
    n = max(ax_totals["n"], bu_totals["n"]) or 1
    print(f"{'TOTALS':<20} {'input tokens':<16} {fmt_tokens(ax_totals['input']):>{col}} {fmt_tokens(bu_totals['input']):>{col}}")
    print(f"{'':20} {'output tokens':<16} {fmt_tokens(ax_totals['output']):>{col}} {fmt_tokens(bu_totals['output']):>{col}}")
    print(f"{'':20} {'total tokens':<16} {fmt_tokens(ax_totals['total']):>{col}} {fmt_tokens(bu_totals['total']):>{col}}")
    print(f"{'':20} {'wall time':<16} {fmt_time(ax_totals['wall_ms']):>{col}} {fmt_time(bu_totals['wall_ms']):>{col}}")

    if ax_totals["total"] and bu_totals["total"]:
        ratio = bu_totals["total"] / ax_totals["total"]
        print(f"\nbrowser-use used {ratio:.1f}x more tokens than ax-distiller")


if __name__ == "__main__":
    main()
