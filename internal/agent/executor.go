package agent

import (
	"context"
	"fmt"
	"log/slog"
)

// Executor handles browser action execution on behalf of worker agents.
// Separates "what to do" (worker decision) from "how to do it" (browser ops).
type Executor struct {
	engine *Engine
	logger *slog.Logger
}

func NewExecutor(engine *Engine, logger *slog.Logger) *Executor {
	return &Executor{engine: engine, logger: logger}
}

// ExecDecision executes an interact-type worker decision against the browser
// and returns the refreshed page structure.
func (e *Executor) ExecDecision(ctx context.Context, d workerDecision) (EngineResult, error) {
	switch {
	case d.ClickNodeID != 0:
		e.logger.Info("executor click", "node_id", d.ClickNodeID)
		if err := e.engine.inter.Click(ctx, d.ClickNodeID); err != nil {
			return EngineResult{}, fmt.Errorf("click node %d: %w", d.ClickNodeID, err)
		}
		e.engine.inter.WaitSettle()

	case d.TypeNodeID != 0:
		e.logger.Info("executor type", "node_id", d.TypeNodeID, "text", d.TypeText)
		if err := e.engine.inter.Type(ctx, d.TypeNodeID, d.TypeText); err != nil {
			return EngineResult{}, fmt.Errorf("type node %d: %w", d.TypeNodeID, err)
		}
		e.engine.inter.WaitSettle()

	default:
		return EngineResult{}, fmt.Errorf("interact action has no node_id set")
	}

	return e.engine.reread(ctx)
}
