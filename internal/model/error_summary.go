package model

import (
	"fmt"
	"regexp"
	"strings"
)

// SummarizeRunError turns a raw agent-run error into a short user-facing
// summary plus the full raw detail. Eino wraps model failures as NodeRunError
// ("[NodeRunError] …\nnode path: [node_1, ChatModel]") and go-openai API
// failures as "error, status code: 400, status: 400 Bad Request, message: …" —
// neither is fit for direct display in a chat timeline. The returned summary
// is a single line; detail is the complete original text (empty when the
// summary already carries all of it).
func SummarizeRunError(err error) (summary, detail string) {
	if err == nil {
		return "", ""
	}
	detail = strings.TrimSpace(err.Error())

	work := nodePathPattern.ReplaceAllString(detail, "")
	work = nodeErrTagPattern.ReplaceAllString(work, "")
	work = strings.TrimSpace(work)

	status := lastSubmatch(statusCodePattern, work)
	apiMsg := lastSubmatch(apiMessagePattern, work)

	switch {
	case status != "" && apiMsg != "":
		summary = fmt.Sprintf("API error %s: %s", status, apiMsg)
	case apiMsg != "":
		summary = apiMsg
	case work != "":
		summary = work
	default:
		summary = detail
	}

	// A 400 rejecting non-text content parts almost always means the selected
	// model has no image-input capability — surface that hint explicitly.
	if status == "400" {
		lower := strings.ToLower(work)
		if strings.Contains(lower, "content.type") || strings.Contains(lower, "image") {
			summary += " — this model may not support image input"
		}
	}

	if r := []rune(summary); len(r) > maxSummaryLen {
		summary = string(r[:maxSummaryLen-1]) + "…"
	}
	if summary == detail {
		detail = ""
	}
	return summary, detail
}

const maxSummaryLen = 300

var (
	// nodePathPattern drops eino's trailing "node path: [node_1, ChatModel]" line.
	nodePathPattern = regexp.MustCompile(`(?m)\s*node path: \[[^\]]*\]\s*$`)
	// nodeErrTagPattern strips eino's leading "[NodeRunError]" / "[NodeError]" tags.
	nodeErrTagPattern = regexp.MustCompile(`^\s*(?:\[Node(?:Run)?Error\]\s*)+`)
	statusCodePattern = regexp.MustCompile(`status code: (\d+)`)
	// apiMessagePattern captures the trailing "message: …" segment produced by
	// go-openai's APIError.Error(); the last occurrence wins for nested wraps.
	apiMessagePattern = regexp.MustCompile(`message: (.+?)\s*$`)
)

// lastSubmatch returns the first capture group of the last match, or "".
func lastSubmatch(re *regexp.Regexp, s string) string {
	matches := re.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSpace(matches[len(matches)-1][1])
}
