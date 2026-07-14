package review

import (
	"encoding/json"
	"strings"
)

// assessment mirrors the reviewer's strict-JSON output contract (see policy.go).
type assessment struct {
	RiskLevel         string `json:"risk_level"`
	UserAuthorization string `json:"user_authorization"`
	Outcome           string `json:"outcome"`
	Rationale         string `json:"rationale"`
}

// parseAssessment extracts the reviewer's JSON verdict from a model reply,
// tolerating code fences and surrounding prose. It returns ok=false when no
// object with a usable "outcome" can be found.
func parseAssessment(text string) (assessment, bool) {
	raw, ok := extractJSONObject(text)
	if !ok {
		return assessment{}, false
	}
	var a assessment
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return assessment{}, false
	}
	if strings.TrimSpace(a.Outcome) == "" {
		return assessment{}, false
	}
	return a, true
}

// extractJSONObject returns the first balanced JSON object in text. It strips a
// leading ```json fence if present, then scans for a brace-balanced span while
// respecting string literals and escapes.
func extractJSONObject(text string) (string, bool) {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}
