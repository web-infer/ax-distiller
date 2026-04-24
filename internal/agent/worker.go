package agent

import (
	"ax-distiller/internal/chrome/heuristic"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
)

const maxWorkerTurns = 20

type workerDecision struct {
	Action      string `json:"action"`
	Findings    string `json:"findings"`
	ClickNodeID int64  `json:"click_node_id"`
	TypeNodeID  int64  `json:"type_node_id"`
	TypeText    string `json:"type_text"`
}

type WorkerResult struct {
	Findings string
}

type Worker struct {
	client   *anthropic.Client
	engine   *Engine
	executor *Executor
	logger   *slog.Logger
	usage    *TokenUsage
}

func NewWorker(client *anthropic.Client, engine *Engine, logger *slog.Logger, usage *TokenUsage) *Worker {
	return &Worker{
		client:   client,
		engine:   engine,
		executor: NewExecutor(engine, logger),
		logger:   logger,
		usage:    usage,
	}
}

func (w *Worker) Run(ctx context.Context, goal, url string) WorkerResult {
	w.logger.Info("worker starting", "url", url)

	res, err := w.engine.Load(ctx, url)
	if err != nil {
		w.logger.Warn("engine load failed", "url", url, "err", err)
		return WorkerResult{}
	}

	currentURL := res.URL
	currentStructure := applyHeuristic(res)
	pageNum := res.PageNum

	for turn := range maxWorkerTurns {
		decision, err := w.decide(ctx, goal, currentURL, pageNum, currentStructure)
		if err != nil {
			w.logger.Warn("decide failed", "turn", turn, "err", err)
			return WorkerResult{}
		}

		w.logger.Info("worker decision", "action", decision.Action, "url", currentURL, "page", pageNum)

		switch decision.Action {
		case "extract":
			return WorkerResult{Findings: decision.Findings}

		case "dead_end":
			return WorkerResult{}

		case "done":
			// model used legacy/confused "done" — treat as extract if findings present
			if decision.Findings != "" {
				w.logger.Warn("worker used 'done' with findings, treating as extract")
				return WorkerResult{Findings: decision.Findings}
			}
			return WorkerResult{}

		case "interact":
			res, err = w.executor.ExecDecision(ctx, decision)
			if err != nil {
				if errors.Is(err, ErrPageLimitReached) {
					w.logger.Warn("page limit reached during navigation")
					return WorkerResult{}
				}
				w.logger.Warn("interact failed", "err", err)
				if res, err = w.engine.reread(ctx); err != nil {
					return WorkerResult{}
				}
				currentURL = res.URL
				pageNum = res.PageNum
				// prepend error AFTER reread so LLM sees both the error and the fresh structure
				currentStructure = "INTERACTION ERROR: " + err.Error() + "\n\n" + applyHeuristic(res)
			} else {
				currentURL = res.URL
				currentStructure = applyHeuristic(res)
				pageNum = res.PageNum
			}

		default:
			w.logger.Warn("unknown action", "action", decision.Action)
			return WorkerResult{}
		}
	}

	return WorkerResult{}
}

// applyHeuristic runs deterministic noise pruning on new page loads and
// re-serializes the pruned tree. In-page interactions (Navigated=false)
// return the pre-serialized structure unchanged.
func applyHeuristic(res EngineResult) string {
	if !res.Navigated || res.Root == nil {
		return res.Structure
	}
	return Serialize(heuristic.Simplify(res.Root), DefaultSerializeOptions())
}

func (w *Worker) decide(ctx context.Context, goal, url string, pageNum int, structure string) (workerDecision, error) {
	prompt := workerUserPrompt(goal, url, pageNum, w.engine.maxPages, structure)

	msg, err := w.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 512,
		System: []anthropic.TextBlockParam{
			{Text: workerSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return workerDecision{}, fmt.Errorf("claude call failed: %w", err)
	}

	if len(msg.Content) == 0 {
		return workerDecision{}, fmt.Errorf("empty response")
	}

	w.usage.Add(msg.Usage)

	raw := stripJSON(msg.Content[0].Text)
	w.logger.Info("worker raw decision", "raw", raw, "in", msg.Usage.InputTokens, "out", msg.Usage.OutputTokens)
	var d workerDecision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return workerDecision{}, fmt.Errorf("json parse failed: %w (raw: %s)", err, raw)
	}
	return d, nil
}
