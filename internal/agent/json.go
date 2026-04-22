package agent

import "strings"

// stripJSON extracts a JSON object from LLM output.
// Handles: raw JSON, ```json fenced, reasoning text + fenced, reasoning text + raw JSON.
func stripJSON(s string) string {
	s = strings.TrimSpace(s)

	if i := strings.Index(s, "```"); i != -1 {
		after := s[i+3:]
		if j := strings.Index(after, "\n"); j != -1 {
			after = after[j+1:]
		}
		if k := strings.LastIndex(after, "```"); k != -1 {
			after = after[:k]
		}
		s = strings.TrimSpace(after)
	}

	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end >= start {
		return strings.TrimSpace(s[start : end+1])
	}

	return s
}
