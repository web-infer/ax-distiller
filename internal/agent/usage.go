package agent

import (
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
)

// TokenUsage accumulates token counts across all LLM calls in a run.
type TokenUsage struct {
	InputTokens  atomic.Int64
	OutputTokens atomic.Int64
}

func (u *TokenUsage) Add(usage anthropic.Usage) {
	u.InputTokens.Add(usage.InputTokens)
	u.OutputTokens.Add(usage.OutputTokens)
}

func (u *TokenUsage) Total() (in, out, total int64) {
	in = u.InputTokens.Load()
	out = u.OutputTokens.Load()
	return in, out, in + out
}
