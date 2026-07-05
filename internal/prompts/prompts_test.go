package prompts

import (
	"strings"
	"testing"
	"time"

	utils "github.com/cnjack/jcode/internal/util"
)

// TestGetSystemPromptSections asserts the static policy sections that must be
// present in the rendered system prompt. A non-empty result also implicitly
// covers template-syntax regressions.
func TestGetSystemPromptSections(t *testing.T) {
	prompt := GetSystemPrompt("darwin", t.TempDir(), "local", nil, "")
	if prompt == "" {
		t.Fatal("GetSystemPrompt returned empty string (template broken?)")
	}

	cases := []struct {
		section string
		substrs []string
	}{
		{
			section: "verification",
			substrs: []string{
				"# Verification",
				"narrowest relevant check",
				"Do not introduce a test framework",
				"4. Verify: run the relevant checks and confirm against real output (see Verification)",
			},
		},
		{
			section: "parallel tool policy",
			substrs: []string{
				"Batch independent tool calls",
				"execute in parallel",
			},
		},
	}
	for _, tc := range cases {
		for _, sub := range tc.substrs {
			if !strings.Contains(prompt, sub) {
				t.Errorf("%s: system prompt missing %q", tc.section, sub)
			}
		}
	}
}

func TestBuildEnvDiffBranchChange(t *testing.T) {
	pwd := t.TempDir()
	stored := SerializeEnvInfo("darwin", pwd, "local", &utils.EnvInfo{GitBranch: "main"})

	diff := BuildEnvDiff(stored, "darwin", pwd, "local", &utils.EnvInfo{GitBranch: "feature"})
	if !strings.Contains(diff, "git_branch: main → feature") {
		t.Errorf("diff missing branch change, got: %q", diff)
	}
	if !strings.Contains(diff, "Environment changes since your context was last updated:") {
		t.Errorf("diff missing generalized header, got: %q", diff)
	}
}

func TestBuildEnvDiffDateChange(t *testing.T) {
	pwd := t.TempDir()
	info := &utils.EnvInfo{GitBranch: "main"}
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	stored := strings.Replace(
		SerializeEnvInfo("darwin", pwd, "local", info),
		"date="+today, "date="+yesterday, 1)

	diff := BuildEnvDiff(stored, "darwin", pwd, "local", info)
	if !strings.Contains(diff, "date: "+yesterday+" → "+today) {
		t.Errorf("diff missing date change, got: %q", diff)
	}
}
