package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var sleepPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsleep\s+(\d+)\s*$`),
	regexp.MustCompile(`\bsleep\s+(\d+)s`),
	regexp.MustCompile(`\bsleep\s+(\d+)m`),
	regexp.MustCompile(`\bsleep\s+(\d+)h`),
}

const maxSleepSeconds = 30

// detectSleep scans the command string (including through pipes, && and ||)
// for sleep invocations that exceed the allowed threshold.
// Returns (true, reason) if the command should be blocked.
func detectSleep(command string) (bool, string) {
	// Split on shell separators to check each segment.
	segments := splitShellSegments(command)

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		for _, pat := range sleepPatterns {
			m := pat.FindStringSubmatch(seg)
			if m == nil {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil || n <= 0 {
				continue
			}
			seconds := toSeconds(n, pat)
			if seconds > maxSleepSeconds {
				return true, fmt.Sprintf(
					"Blocked: sleep %ds exceeds the %ds limit. "+
						"Remove or reduce the sleep duration.",
					seconds, maxSleepSeconds,
				)
			}
		}
	}
	return false, ""
}

// toSeconds converts the captured number to seconds based on the pattern's
// unit suffix (bare/s = seconds, m = minutes, h = hours).
func toSeconds(n int, pat *regexp.Regexp) int {
	s := pat.String()
	if strings.HasSuffix(s, `m`) {
		return n * 60
	}
	if strings.HasSuffix(s, `h`) {
		return n * 3600
	}
	return n
}

// splitShellSegments splits a command on &&, ||, ;, and | so each part
// can be checked independently.
func splitShellSegments(cmd string) []string {
	// Replace multi-char separators first, then split on single char.
	cmd = strings.ReplaceAll(cmd, "&&", "\x00")
	cmd = strings.ReplaceAll(cmd, "||", "\x00")
	cmd = strings.ReplaceAll(cmd, ";", "\x00")
	cmd = strings.ReplaceAll(cmd, "|", "\x00")
	return strings.Split(cmd, "\x00")
}
