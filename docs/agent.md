# Agent Workflow

LLM-driven web traversal built on top of the AX heuristics engine. Lives on `feat/agent-demo` branch. Entry point: `cmd/demo-agent`.

## Overview

The agent workflow lets Claude autonomously traverse websites by feeding it the compressed accessibility tree instead of raw HTML. Each page visit goes through the heuristics engine first — the LLM never sees DOM directly.

**Constraints:**
- Max 10 pages per run (hard cap, enforced in engine)
- Max 3 concurrent worker agents
- All LLM inference via Anthropic API (remote, CPU-only)
- URL pipeline is sequential + single-process (mutex-protected)
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
│  - work queue (URL + goal)        │
│  - pool of ≤3 Worker goroutines   │
│  - URL conflict guard (dedup map) │
│  - collects findings              │
│  - 1 final LLM call: synthesize  │
└────────────┬─────────────────────┘
             │ WorkItem{goal, url}
    ┌────────┼────────┐
    ▼        ▼        ▼
┌────────┐ ┌────────┐ ┌────────┐
│Worker 1│ │Worker 2│ │Worker 3│  ← identical, stateless
└───┬────┘ └───┬────┘ └───┬────┘
    │           │           │
    └───────────┴───────────┘
                │
                ▼
  ┌─────────────────────────────┐
  │   Heuristics Engine          │  ← MUTEX-PROTECTED, SEQUENTIAL
  │   navigate → EVENT_RESET    │
  │   + 2s EVENT_PATCH drain    │
  │   → Persistent.HandleEvent  │
  │   → Serialize(Root)         │
  └─────────────────────────────┘
```

---

## Components

### Agent Spawner (`internal/agent/spawner.go`)

Coordinates the full run with exactly 3 LLM interactions total (intent + N worker decisions + synthesize):

1. **Intent parse** (1 LLM call) — `ParseIntent(task) → {goal, start_urls[]}`
2. **Worker pool** — spawns up to 3 goroutines pulling `WorkItem`s from a buffered channel
3. **Synthesize** (1 LLM call) — `Synthesize(goal, findings[]) → string`

Work queue uses an active-item counter to detect completion — workers can add new URLs while processing without deadlock.

### Worker Agent (`internal/agent/worker.go`)

Stateless — no conversation history carried between URLs. Per-URL loop (max 5 turns):

1. Call `engine.Load(url)` → serialized AX structure text
2. Single LLM call: `Decide(goal, structure) → Decision`
3. Dispatch on `Decision.Action`:

| Action | Behavior |
|--------|----------|
| `extract` | Return findings to spawner, done |
| `follow_urls` | Enqueue new URLs in spawner, done |
| `interact` | Execute click/type via Executor, loop back with fresh structure |
| `done` | Signal no findings, done |

Uses `claude-haiku-4-5` — cheapest model, fast, sufficient for structured JSON decisions.

### Executor (`internal/agent/executor.go`)

Separates "what to do" (Worker decision) from "how to do it" (browser operations).

```go
func (e *Executor) ExecDecision(ctx context.Context, d workerDecision) (EngineResult, error)
```

Handles `click_node_id` and `type_node_id` + `type_text` from worker decisions. After each interaction: `WaitSettle()` + `engine.reread()` to get updated structure.

### Heuristics Engine (`internal/agent/engine.go`)

The bridge between agents and the browser. **The only place that touches the browser.**

```go
func (e *Engine) Load(ctx context.Context, url string) (EngineResult, error)
```

Sequential execution (mutex-locked):
1. Check page budget (`pageCount >= maxPages`) → `ErrPageLimitReached`
2. Check URL dedup (`visited[url]`) → `ErrAlreadyVisited`
3. `interact.Navigate(url)`
4. Drain events until `EVENT_RESET` (10s timeout)
5. Drain `EVENT_PATCH` events for 2s — fills in the sparse post-reset tree
6. `Serialize(persistent.Root)` → text
7. Return `EngineResult{URL, Structure, PageNum}`

### Serializer (`internal/agent/serializer.go`)

Converts `*structure.Structure` → indented text the LLM can reason about.

```
[1234] button: "Add to Cart"
[1235] link: "See all results"
SYNTHETIC_LIST:
  [1236] listitem: "Electronics"
  [1236] listitem: "Books"
```

- `[ID]` = `BackendDOMNodeID` — stable, usable in click/type tool calls
- `SYNTHETIC_LIST` / `SYNTHETIC_OBJECT` = compressed wrappers, no ID, not clickable
- Names truncated to 60 chars; max 300 lines; max depth 10

---

## URL Pipeline (Sequential)

Every time any agent needs a new page:

```
Agent calls engine.Load(url)
  → mutex.Lock()  (blocks other workers)
  → inter.Navigate(url)
  → drain EVENT_RESET (10s timeout)
  → drain EVENT_PATCHes (2s)
  → persistent.HandleEvent(events...)
  → Serialize(persistent.Root) → text
  → mutex.Unlock()
  → return text to agent
```

Workers never call `inter.Navigate` directly — all browser access goes through `Engine.Load` or `Engine.reread`.

---

## Token Budget (approximate)

| Step | Calls | ~Tokens each | ~Total |
|------|-------|-------------|--------|
| Intent parse | 1 | 500 | 500 |
| Worker Decide | ≤50 (10 pages × 5 turns) | 800 | 40k |
| Synthesize | 1 | 2000 | 2000 |
| **Total** | | | **~43k** |

---

## File Reference

```
cmd/demo-agent/main.go               entry point, CLI flags
internal/agent/
  spawner.go                         intent → worker pool → synthesize
  worker.go                          stateless per-URL agent loop
  executor.go                        executes browser actions (click/type)
  engine.go                          mutex-protected AX engine pipeline
  serializer.go                      *structure.Structure → LLM text
  prompt.go                          system prompts for spawner + worker
  json.go                            stripJSON (strips markdown fences)
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
