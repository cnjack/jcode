package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExecStreams holds separate and aggregated command output for UI consumers.
type ExecStreams struct {
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Aggregated string `json:"aggregated,omitempty"`
}

// ExecMeta holds structured execution metadata for UI consumers.
type ExecMeta struct {
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	SpillPath  string `json:"spill_path,omitempty"`
}

// ExecPresentation carries UI presentation hints for an execute result.
type ExecPresentation struct {
	Kind        string `json:"kind"` // read | search | list | shell | edit | agent | other
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Collapsible bool   `json:"collapsible"`
}

// ExecResult is the dual-channel execute result: ModelOutput is what the LLM
// sees; Streams/Meta/Presentation/DisplayBody are structured UI fields.
type ExecResult struct {
	ModelOutput  string
	DisplayBody  string // clean body without STDOUT:/STDERR: labels or footers
	Streams      ExecStreams
	Meta         ExecMeta
	Presentation ExecPresentation
}

var (
	execExitCodeRe = regexp.MustCompile(`\[Exit code: (-?\d+)\]`)
	execDurationRe = regexp.MustCompile(`\[Completed in ([0-9.]+)s\]`)
	execSpillRe    = regexp.MustCompile(`\[Full output: (.+?)\]`)
)

// BuildExecResult formats raw stdout/stderr into a model-facing string and a
// structured UI payload. Truncation and spill file behavior match the historical
// execute tool contract so existing tests and models keep working.
func BuildExecResult(stdout, stderr string, runErr error, elapsed time.Duration, command string) ExecResult {
	stdoutBody, stdoutDropped, _ := truncateHeadTail(stdout, execStdoutHeadBytes, execStdoutTailBytes)
	stderrBody, stderrDropped, _ := truncateHeadTail(stderr, execStderrHeadBytes, execStderrTailBytes)
	truncated := stdoutDropped > 0 || stderrDropped > 0
	spillPath := ""
	if truncated {
		spillPath = spillExecOutput(stdout, stderr)
	}

	exitCode := 0
	if runErr != nil {
		exitCode = -1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	model := formatExecModelString(stdoutBody, stderrBody, spillPath, exitCode, elapsed, runErr != nil)
	aggregated := joinStreams(stdoutBody, stderrBody)
	displayBody := cleanExecBody(stdoutBody, stderrBody)
	cat := classifyCommand(command)

	return ExecResult{
		ModelOutput: model,
		DisplayBody: displayBody,
		Streams: ExecStreams{
			Stdout:     stdoutBody,
			Stderr:     stderrBody,
			Aggregated: aggregated,
		},
		Meta: ExecMeta{
			ExitCode:   exitCode,
			DurationMs: elapsed.Milliseconds(),
			Truncated:  truncated,
			SpillPath:  spillPath,
		},
		Presentation: presentationForCategory(cat, command),
	}
}

// presentationForCategory maps a command category to UI presentation hints.
func presentationForCategory(cat CommandCategory, command string) ExecPresentation {
	var kind, title string
	var collapsible bool
	switch cat {
	case CmdRead:
		kind, collapsible, title = "read", true, "Read"
	case CmdSearch:
		kind, collapsible, title = "search", true, "Search"
	case CmdList:
		kind, collapsible, title = "list", true, "List"
	case CmdGit:
		kind, collapsible, title = "search", true, "Git"
	case CmdSafe:
		kind, collapsible, title = "list", true, "Shell"
	default:
		kind, collapsible, title = "shell", false, "Shell"
	}
	subtitle := command
	if len(subtitle) > 100 {
		subtitle = subtitle[:100] + "…"
	}
	return ExecPresentation{
		Kind:        kind,
		Title:       title,
		Subtitle:    subtitle,
		Collapsible: collapsible,
	}
}

// ClassifyCommand is the exported form of classifyCommand for handlers/UI.
func ClassifyCommand(cmd string) CommandCategory {
	return classifyCommand(cmd)
}

// PresentationKindForTool returns a stable presentation kind for any tool name.
func PresentationKindForTool(name, argsJSON string) (kind string, collapsible bool) {
	switch name {
	case "read", "todoread", "load_skill":
		return "read", true
	case "grep", "glob":
		return "search", true
	case "write", "edit", "multi_edit", "todowrite":
		return "edit", false
	case "subagent", "team_spawn":
		return "agent", false
	case "execute", "background":
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		cmd, _ := args["command"].(string)
		p := presentationForCategory(classifyCommand(cmd), cmd)
		return p.Kind, p.Collapsible
	default:
		return "other", false
	}
}

// formatExecModelString builds the historical model-facing execute string.
func formatExecModelString(stdoutBody, stderrBody, spillPath string, exitCode int, elapsed time.Duration, failed bool) string {
	var result strings.Builder
	if stdoutBody != "" {
		result.WriteString("STDOUT:\n")
		result.WriteString(stdoutBody)
	}
	if stderrBody != "" {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("STDERR:\n")
		result.WriteString(stderrBody)
	}
	if spillPath != "" {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		fmt.Fprintf(&result, "[Full output: %s]", spillPath)
	}

	if failed {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		fmt.Fprintf(&result, "[Exit code: %d]\n", exitCode)
		fmt.Fprintf(&result, "[Completed in %.1fs]", elapsed.Seconds())
		if elapsed > bgHintThreshold {
			fmt.Fprintf(&result,
				"\n[Hint: command took %.0fs. Consider using background=true for long-running commands.]",
				elapsed.Seconds(),
			)
		}
		return result.String()
	}

	if result.Len() == 0 {
		result.WriteString("Command executed successfully (no output)")
	}
	fmt.Fprintf(&result, "\n[Completed in %.1fs]", elapsed.Seconds())
	if elapsed > bgHintThreshold {
		fmt.Fprintf(&result,
			"\n[Hint: command took %.0fs. Consider using background=true for long-running commands.]",
			elapsed.Seconds(),
		)
	}
	return result.String()
}

func joinStreams(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

func cleanExecBody(stdout, stderr string) string {
	return strings.TrimRight(joinStreams(stdout, stderr), "\n")
}

// ParseExecModelOutput reconstructs structured UI fields from a model-facing
// execute string. Used by transports that only receive the LLM result string.
// Returns ok=false when the string does not look like execute output.
func ParseExecModelOutput(output string) (ExecResult, bool) {
	if output == "" {
		return ExecResult{}, false
	}
	hasLabel := strings.Contains(output, "STDOUT:") || strings.Contains(output, "STDERR:")
	hasFooter := strings.Contains(output, "[Completed in") || strings.Contains(output, "[Exit code:")
	hasEmptyOK := strings.HasPrefix(output, "Command executed successfully (no output)")
	if !hasLabel && !hasFooter && !hasEmptyOK {
		return ExecResult{}, false
	}

	stdout, stderr := splitLabeledStreams(output)
	exitCode := 0
	if m := execExitCodeRe.FindStringSubmatch(output); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			exitCode = n
		}
	}
	durationMs := int64(0)
	if m := execDurationRe.FindStringSubmatch(output); len(m) == 2 {
		if sec, err := strconv.ParseFloat(m[1], 64); err == nil {
			durationMs = int64(sec * 1000)
		}
	}
	spillPath := ""
	if m := execSpillRe.FindStringSubmatch(output); len(m) == 2 {
		spillPath = m[1]
	}
	truncated := strings.Contains(output, "output truncated") || spillPath != ""

	return ExecResult{
		ModelOutput: output,
		DisplayBody: cleanExecBody(stdout, stderr),
		Streams: ExecStreams{
			Stdout:     stdout,
			Stderr:     stderr,
			Aggregated: joinStreams(stdout, stderr),
		},
		Meta: ExecMeta{
			ExitCode:   exitCode,
			DurationMs: durationMs,
			Truncated:  truncated,
			SpillPath:  spillPath,
		},
		Presentation: ExecPresentation{Kind: "shell", Title: "Shell"},
	}, true
}

// splitLabeledStreams extracts STDOUT/STDERR bodies from a model-facing string.
// Footer lines ([Exit code], [Completed], [Hint], [Full output]) are excluded.
func splitLabeledStreams(output string) (stdout, stderr string) {
	lines := strings.Split(output, "\n")
	var cur *strings.Builder
	var outB, errB strings.Builder

	for _, line := range lines {
		switch {
		case line == "STDOUT:":
			cur = &outB
			continue
		case line == "STDERR:":
			cur = &errB
			continue
		case strings.HasPrefix(line, "[Exit code:"),
			strings.HasPrefix(line, "[Completed in"),
			strings.HasPrefix(line, "[Hint:"),
			strings.HasPrefix(line, "[Full output:"),
			strings.HasPrefix(line, "Command executed successfully"):
			cur = nil
			continue
		}
		if cur != nil {
			if cur.Len() > 0 {
				cur.WriteByte('\n')
			}
			cur.WriteString(line)
		}
	}
	return outB.String(), errB.String()
}
