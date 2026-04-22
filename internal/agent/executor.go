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
		nodeID := e.resolveNode(ctx, d.ClickNodeID, "", "")
		e.logger.Info("executor click", "node_id", nodeID)
		if err := e.engine.inter.Click(ctx, nodeID); err != nil {
			return EngineResult{}, fmt.Errorf("click node %d: %w", nodeID, err)
		}
		e.engine.inter.WaitSettle()

	case d.TypeNodeID != 0:
		nodeID := e.resolveNode(ctx, d.TypeNodeID, "searchbox", "")
		e.logger.Info("executor type", "node_id", nodeID, "text", d.TypeText)
		if err := e.engine.inter.Type(ctx, nodeID, d.TypeText); err != nil {
			return EngineResult{}, fmt.Errorf("type node %d: %w", nodeID, err)
		}
		// auto-submit after typing
		if err := e.engine.inter.PressKey(ctx, "Enter"); err != nil {
			e.logger.Warn("executor press enter failed", "err", err)
		}
		e.engine.inter.WaitSettle()

	default:
		return EngineResult{}, fmt.Errorf("interact action has no node_id set")
	}

	return e.engine.reread(ctx)
}

// resolveNode uses QueryAXTree to confirm the node exists live.
// Falls back to hintRole query if the original ID resolves to nothing,
// otherwise returns the original ID unchanged.
func (e *Executor) resolveNode(ctx context.Context, nodeID int64, hintRole, hintName string) int64 {
	if hintRole == "" && hintName == "" {
		return nodeID
	}
	found, err := e.engine.inter.FindNode(ctx, hintRole, hintName)
	if err != nil || found == 0 {
		return nodeID
	}
	e.logger.Info("executor resolved node via AX query", "original", nodeID, "resolved", found, "role", hintRole)
	return found
}
