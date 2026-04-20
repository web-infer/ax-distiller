package agent

import "strings"

// stripJSON removes markdown code fences that LLMs sometimes wrap JSON in.
func stripJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// remove first line (```json or ```)
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		// remove trailing ```
		if i := strings.LastIndex(s, "```"); i != -1 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
