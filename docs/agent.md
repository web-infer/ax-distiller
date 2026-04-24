# Agent Workflow

LLM-driven web traversal built on top of the AX heuristics engine. Lives on `feat/agent-demo` branch. Entry point: `cmd/demo-agent`.

## Overview

The agent workflow lets Claude autonomously traverse websites by feeding it the compressed, goal-filtered accessibility tree instead of raw HTML. Each new page load goes through the pruning agent first — the worker LLM never sees noise sections irrelevant to the goal.

**Constraints:**
- Max 10 pages per run (hard cap, enforced in engine)
- Single sequential worker (one page at a time, one browser)
- All LLM inference via Anthropic API (remote, CPU-only)
- No duplicate URL visits (zero-cost dedup map)

---

## Architecture

```
User task
    │
    ▼
┌──────────────────────────────────┐
│         Agent Spawner             │
│  - 1 LLM call: parse intent      │
│    → goal + starting URLs         │
│  - sequential URL loop            │
│  - single Worker                  │
│  - collects findings              │
│  - 1 final LLM call: synthesize  │
└────────────┬─────────────────────┘
             │ goal + url (sequential)
             ▼
        ┌────────┐
        │ Worker │  ← stateless, one URL at a time
        └───┬────┘
            │ new page detected
            ▼
  ┌─────────────────────────────┐
  │   Pruning Agent              │  ← goal-aware AX tree filter
  │   Summarize(root) → 2-level  │
  │   LLM call: prune_ids[]      │
  │   PruneByIDs(root, ids)      │
  │   → pruned *Structure        │
  │   → Serialize → LLM text     │
  └─────────────────────────────┘
            │ (in-page interactions skip pruner)
            ▼
  ┌─────────────────────────────┐
  │   Engine (browser)           │  ← MUTEX-PROTECTED, SEQUENTIAL
  │   navigate → EVENT_RESET    │
  │   + 2s EVENT_PATCH drain    │
  │   → Persistent.HandleEvent  │
  │   → EngineResult{Root, ...} │
  └─────────────────────────────┘
```

---

## Components

### Agent Spawner (`internal/agent/spawner.go`)

Coordinates the full run:

1. **Intent parse** (1 LLM call) — `ParseIntent(task) → {goal, start_urls[]}`
2. **Sequential loop** — iterates `start_urls`, runs Worker on each in order
3. **Synthesize** (1 LLM call) — `Synthesize(goal, findings[]) → string`

Single worker eliminates the browser race condition that existed with the prior worker pool (all workers shared one browser; concurrent clicks hit the wrong page).

### Worker Agent (`internal/agent/worker.go`)

Stateless — no conversation history carried between URLs. Per-URL loop (max 20 turns):

1. Call `engine.Load(url)` → `EngineResult{Root, Navigated=true, ...}`
2. On `Navigated=true`: call `Pruner.Prune(goal, root)` → pruned `*Structure` → serialize
3. Single LLM call: `Decide(goal, structure) → Decision`
4. Dispatch on `Decision.Action`:

| Action | Behavior |
|--------|----------|
| `extract` | Return findings to spawner, done |
| `interact` | Execute click/type via Executor, loop back with fresh structure |
| `dead_end` | Signal no findings, done |
| `done` | Legacy fallback — rescued as extract if findings present |

Uses `claude-haiku-4-5` — cheapest model, fast, sufficient for structured JSON decisions.

### Pruning Agent (`internal/agent/pruner.go`)

Goal-aware AX tree filter called once per new page load.

1. `heuristic.Summarize(root)` — generates a shallow 2-level section overview (role + name, no full tree)
2. Single Haiku LLM call (128 max tokens): receives goal + section summary, returns `{"prune_ids": [...]}`
3. `heuristic.PruneByIDs(root, ids)` — builds pruned copy of tree (original untouched)
4. Falls back to original root on any error — never drops content incorrectly

**Conservative by design:** system prompt instructs to keep sections when uncertain. Only prunes sections with zero relevance to the goal (keyboard shortcut menus, footers, unrelated promotional regions, etc.).

In-page interactions (`Navigated=false`) skip the pruner — structure returned as-is.

### Heuristic Package (`internal/chrome/heuristic/heuristic.go`)

Two pure functions operating on `*structure.Structure`:

- `Summarize(root) string` — 2-level depth walk, skips SYNTHETIC wrappers, emits `[ID] role: "name"` per section
- `PruneByIDs(root, ids map[int64]bool) *structure.Structure` — non-destructive copy with pruned subtrees omitted

### Executor (`internal/agent/executor.go`)

Separates "what to do" (Worker decision) from "how to do it" (browser operations).

```go
func (e *Executor) ExecDecision(ctx context.Context, d workerDecision) (EngineResult, error)
```

Handles `click_node_id` and `type_node_id` + `type_text` from worker decisions. After each interaction: `WaitSettle()` + `engine.reread()` to get updated structure. Sets `Navigated=true` on result only when a link click caused a page navigation (detected via `EVENT_RESET`).

### Engine (`internal/agent/engine.go`)

The bridge between agents and the browser. **The only place that touches the browser.**

```go
func (e *Engine) Load(ctx context.Context, url string) (EngineResult, error)
```

Sequential execution (mutex-locked):
1. Check page budget (`pageCount >= maxPages`) → `ErrPageLimitReached`
2. Check URL dedup (`visited[url]`) → `ErrAlreadyVisited`
3. `interact.Navigate(url)`
4. Drain events until `EVENT_RESET` (15s timeout)
5. Drain `EVENT_PATCH` events for 2s — fills in the sparse post-reset tree
6. Return `EngineResult{URL, Structure, PageNum, Root, Navigated: true}`

`EngineResult` fields:

| Field | Type | Description |
|-------|------|-------------|
| `URL` | `string` | Current page URL |
| `Structure` | `string` | Pre-serialized full tree (debug dumps, non-navigated rerenders) |
| `PageNum` | `int` | Page number within budget |
| `Root` | `*structure.Structure` | Raw tree root for heuristic access |
| `Navigated` | `bool` | True when a new page was loaded |

### Serializer (`internal/agent/serializer.go`)

Converts `*structure.Structure` → indented text the LLM can reason about.

```
[1234] button: "Add to Cart"
[1235] link: "See all results"
SYNTHETIC_LIST:
  [1236] listitem: "Electronics"
  [1237] listitem: "Books"
```

- `[ID]` = `BackendDOMNodeID` — stable, usable in click/type decisions
- `SYNTHETIC_LIST` / `SYNTHETIC_OBJECT` = compressed wrappers, no ID, not clickable
- Names truncated to 80 chars; max 500 lines; max depth 12

---

## Stop Conditions

Worker exits early on:
- `extract` action (found answer)
- `dead_end` action (no path forward)
- `ErrPageLimitReached` from engine
- Turn limit (20 turns)

Model guidance in system prompt:
- On last page (`page N/N`): must output `extract` or `dead_end` — no further navigation
- `dead_end` = stuck with no relevant content AND no navigation path (not "task complete")
- `extract` = only when the actual answer is present, not a navigation step

---

## Token Budget (approximate)

| Step | Calls | ~Tokens each | ~Total |
|------|-------|-------------|--------|
| Intent parse | 1 | 500 | 500 |
| Pruner | ≤10 (one per page) | 300 | 3k |
| Worker Decide | ≤20 (per URL, 20 turns) | 800 | 16k |
| Synthesize | 1 | 2000 | 2000 |
| **Total** | | | **~22k** |

---

## File Reference

```
cmd/demo-agent/main.go               entry point, CLI flags
internal/agent/
  spawner.go                         intent → sequential worker loop → synthesize
  worker.go                          stateless per-URL agent loop
  pruner.go                          goal-aware LLM pruning agent
  executor.go                        executes browser actions (click/type)
  engine.go                          mutex-protected AX engine pipeline
  serializer.go                      *structure.Structure → LLM text
  prompt.go                          system prompts for spawner + worker
  json.go                            stripJSON (strips markdown fences)
  usage.go                           shared atomic token counter
internal/chrome/heuristic/
  heuristic.go                       Summarize + PruneByIDs (pure, no LLM)
internal/chrome/interact/
  interact.go                        Click, Type, Navigate, PressKey via rod
```

---

## Running

```bash
export ANTHROPIC_API_KEY=sk-ant-...

# basic
go run ./cmd/demo-agent "Find the price of the first product on amazon.com"

# headless, fewer pages
go run ./cmd/demo-agent -headless -max-pages 3 "Top headline on bbc.com"

# verbose debug output
go run ./cmd/demo-agent -verbose "..."
```
