package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
)

const maxWorkerTurns = 5

type workerDecision struct {
	Action      string   `json:"action"`
	Findings    string   `json:"findings"`
	URLs        []string `json:"urls"`
	ClickNodeID int64    `json:"click_node_id"`
	TypeNodeID  int64    `json:"type_node_id"`
	TypeText    string   `json:"type_text"`
}

type WorkerResult struct {
	Findings string
	NewURLs  []string
}

type Worker struct {
	client   *anthropic.Client
	engine   *Engine
	executor *Executor
	logger   *slog.Logger
}

func NewWorker(client *anthropic.Client, engine *Engine, logger *slog.Logger) *Worker {
	return &Worker{
		client:   client,
		engine:   engine,
		executor: NewExecutor(engine, logger),
		logger:   logger,
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
	currentStructure := res.Structure
	pageNum := res.PageNum

	for turn := range maxWorkerTurns {
		decision, err := w.decide(ctx, goal, currentURL, pageNum, currentStructure)
		if err != nil {
			w.logger.Warn("decide failed", "turn", turn, "err", err)
			return WorkerResult{}
		}

		w.logger.Info("worker decision", "action", decision.Action, "url", currentURL)

		switch decision.Action {
		case "extract":
			return WorkerResult{Findings: decision.Findings}

		case "follow_urls":
			return WorkerResult{NewURLs: decision.URLs}

		case "done":
			return WorkerResult{}

		case "interact":
			interactErr := ""
			if decision.ClickNodeID != 0 {
				if err := w.engine.inter.Click(ctx, decision.ClickNodeID); err != nil {
					w.logger.Warn("click failed", "node_id", decision.ClickNodeID, "err", err)
					interactErr = fmt.Sprintf("click node %d failed: %v", decision.ClickNodeID, err)
				} else {
					w.engine.inter.WaitSettle()
				}
			} else if decision.TypeNodeID != 0 {
				if err := w.engine.inter.Type(ctx, decision.TypeNodeID, decision.TypeText); err != nil {
					w.logger.Warn("type failed", "node_id", decision.TypeNodeID, "err", err)
					interactErr = fmt.Sprintf("type node %d failed: %v", decision.TypeNodeID, err)
				} else {
					w.engine.inter.WaitSettle()
				}
			}
			// re-read regardless (interaction may have partially worked)
			res, err = w.engine.reread(ctx)
			if err != nil {
				w.logger.Warn("reread failed", "err", err)
				return WorkerResult{}
			}
			currentStructure = res.Structure
			if interactErr != "" {
				// prepend error so next decide call knows what failed
				currentStructure = "INTERACTION ERROR: " + interactErr + "\n\n" + currentStructure
			}
			pageNum = res.PageNum

		default:
			w.logger.Warn("unknown action", "action", decision.Action)
			return WorkerResult{}
		}
	}

	return WorkerResult{}
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

	raw := stripJSON(msg.Content[0].Text)
	w.logger.Info("worker raw decision", "raw", raw)
	var d workerDecision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return workerDecision{}, fmt.Errorf("json parse failed: %w (raw: %s)", err, raw)
	}
	w.logger.Info("worker decision parsed", "action", d.Action, "findings", d.Findings, "urls", d.URLs)
	return d, nil
}
