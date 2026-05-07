#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
OUT="$SCRIPT_DIR/results/ax_results.jsonl"
MAX_PAGES="${MAX_PAGES:-3}"

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "Error: ANTHROPIC_API_KEY not set" >&2
  exit 1
fi

mkdir -p "$SCRIPT_DIR/results"
> "$OUT"

prompts=$(python3 -c "import json,sys; data=json.load(open('$SCRIPT_DIR/prompts.json')); [print(p['id']+'|||'+p['prompt']) for p in data]")

while IFS='|||' read -r id prompt; do
  echo "=== [$id] $prompt ==="

  tmplog=$(mktemp)
  start=$(date +%s%N)

  go run "$ROOT_DIR/cmd/demo-agent" -max-pages "$MAX_PAGES" -headless "$prompt" \
    >"$tmplog" 2>&1 || true

  end=$(date +%s%N)
  wall_ms=$(( (end - start) / 1000000 ))

  input_tok=$(grep "token usage" "$tmplog" | grep -oP 'input=\K[0-9]+' | tail -1 || echo 0)
  output_tok=$(grep "token usage" "$tmplog" | grep -oP 'output=\K[0-9]+' | tail -1 || echo 0)
  total_tok=$(grep "token usage" "$tmplog" | grep -oP 'total=\K[0-9]+' | tail -1 || echo 0)
  turns=$(grep -c "worker decision" "$tmplog" || echo 0)
  pages=$(grep -c "engine: navigating" "$tmplog" || echo 0)
  result=$(awk '/^Result:/{found=1; next} found{print}' "$tmplog" | head -10 | tr '\n' ' ' | sed 's/"/\\"/g')

  cat >> "$OUT" <<EOF
{"tool":"ax-distiller","id":"$id","prompt":"$prompt","input_tokens":$input_tok,"output_tokens":$output_tok,"total_tokens":$total_tok,"wall_ms":$wall_ms,"turns":$turns,"pages":$pages,"result":"$result"}
EOF

  echo "  tokens=$total_tok  time=${wall_ms}ms  turns=$turns  pages=$pages"
  echo "  result: $result"
  echo ""

  rm -f "$tmplog"
done <<< "$prompts"

echo "Results written to $OUT"
