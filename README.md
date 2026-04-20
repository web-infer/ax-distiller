# ax-distiller

> Streams Chrome's accessibility tree as compressed structural mutations, enabling LLMs to traverse and understand web pages efficiently.

## Demos

### `demo-agent` — LLM web traversal agent

Multi-agent workflow that uses the AX heuristics engine to feed page structure to Claude.

**Requirements:** `ANTHROPIC_API_KEY` env var set.

```bash
# basic usage
go run ./cmd/demo-agent "Find the price of the first product on amazon.com"

# flags
go run ./cmd/demo-agent -headless -max-pages 5 "Top headline on bbc.com"
go run ./cmd/demo-agent -chrome "/path/to/chrome" -verbose "..."

# help
go run ./cmd/demo-agent -help
```

| Flag | Default | Description |
|------|---------|-------------|
| `-headless` | `false` | Run Chrome without a visible window |
| `-max-pages` | `10` | Max pages to visit (1–10) |
| `-chrome` | auto-detected | Path to Chrome/Chromium binary |
| `-verbose` | `false` | Debug logging |

### `demo-axstream-persistent` — AX stream + structure viewer

```bash
go run ./cmd/demo-axstream-persistent
```

### `demo-axstream` — raw AX event stream

```bash
go run ./cmd/demo-axstream
```

## Roadmap

1. [x] Migrate code from [experiments](https://github.com/web-infer/experiments.git)
2. [x] Ensure working setup / demo for axstream.
3. [x] Ensure structure detection & etc... works
4. [x] Implement persistent structure.
5. [x] Implement agent workflow for LLM web traversal (`feat/agent-demo`).
6. [ ] Implement methods for interacting with nodes.
7. [ ] Create a simple scraping demo.
8. [ ] Implement devtools.

## Docs

- [Architecture](docs/architecture.md)
- [Agent workflow](docs/agent.md)
- [Design patterns](docs/README.md)
- [Structures](docs/structures.md)

