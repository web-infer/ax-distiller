package agent

import (
	"ax-distiller/internal/structure"
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

// resolveNode refreshes a BackendDOMNodeID via a live QueryAXTree lookup.
// For clicks, it looks up role+name from the persistent tree so that if the
// page regenerated the element (new BackendDOMNodeID, same role+name), the
// fresh ID is used for DOM.getBoxModel. Falls back to original ID on any failure.
func (e *Executor) resolveNode(ctx context.Context, nodeID int64, hintRole, hintName string) int64 {
	role, name := hintRole, hintName
	if role == "" && name == "" {
		role, name = e.lookupNode(nodeID)
	}
	if role == "" && name == "" {
		return nodeID
	}
	found, err := e.engine.inter.FindNode(ctx, role, name)
	if err != nil || found == 0 {
		return nodeID
	}
	e.logger.Info("executor resolved node via AX query", "original", nodeID, "resolved", found, "role", role, "name", name)
	return found
}

// lookupNode walks the persistent structure tree to find role+name for a BackendDOMNodeID.
func (e *Executor) lookupNode(backendNodeID int64) (role, name string) {
	root := e.engine.persistent.Root
	if root == nil {
		return
	}
	stack := []*structure.Structure{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if int64(n.Underlying.BackendDOMNodeID) == backendNodeID {
			return n.Underlying.Role.Value, n.Underlying.Name.Value
		}
		if n.NextSibling != nil {
			stack = append(stack, n.NextSibling)
		}
		if n.FirstChild != nil {
			stack = append(stack, n.FirstChild)
		}
	}
	return
}
