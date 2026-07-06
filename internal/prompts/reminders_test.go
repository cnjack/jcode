package prompts

import (
	"strings"
	"testing"
)

func TestExternalFileChangedReminder(t *testing.T) {
	cases := []struct {
		name        string
		rc          ReminderContext
		fire        bool
		mustContain []string
	}{
		{
			name:        "changed file fires with re-read instruction",
			rc:          ReminderContext{ExternalChangedFiles: []string{"/a.go"}},
			fire:        true,
			mustContain: []string{"re-read", "/a.go"},
		},
		{
			name:        "gone file fires with deleted marker",
			rc:          ReminderContext{ExternalGoneFiles: []string{"/gone.go"}},
			fire:        true,
			mustContain: []string{"/gone.go (deleted)"},
		},
		{
			name: "empty slices do not fire",
			rc:   ReminderContext{},
			fire: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Join(CollectReminders(&tc.rc), "\n")
			if !tc.fire {
				if strings.Contains(text, "External file changes") {
					t.Fatalf("reminder fired unexpectedly: %q", text)
				}
				return
			}
			if !strings.Contains(text, "External file changes") {
				t.Fatalf("reminder did not fire: %q", text)
			}
			for _, sub := range tc.mustContain {
				if !strings.Contains(text, sub) {
					t.Errorf("missing %q in %q", sub, text)
				}
			}
		})
	}
}

func TestEnvDriftReminder(t *testing.T) {
	diff := "Environment changes since your context was last updated:\ngit_branch: a → b"
	msgs := CollectReminders(&ReminderContext{EnvDiff: diff})
	if len(msgs) != 1 || msgs[0] != diff {
		t.Fatalf("env_drift should pass the diff through verbatim, got: %v", msgs)
	}
	if msgs := CollectReminders(&ReminderContext{}); len(msgs) != 0 {
		t.Fatalf("env_drift must not fire on empty diff, got: %v", msgs)
	}
}

func TestAgentsMdChangedReminder(t *testing.T) {
	// Updated content is injected with supersedes wording.
	text := strings.Join(CollectReminders(&ReminderContext{AgentsMdUpdate: "new project rules"}), "\n")
	if !strings.Contains(text, "supersedes") || !strings.Contains(text, "new project rules") {
		t.Fatalf("agents_md_changed missing supersedes wording or content: %q", text)
	}

	// Removal notice.
	text = strings.Join(CollectReminders(&ReminderContext{AgentsMdRemoved: true}), "\n")
	if !strings.Contains(text, "AGENTS.md was removed") {
		t.Fatalf("agents_md_changed missing removal notice: %q", text)
	}

	// Long content is truncated with a marker.
	long := strings.Repeat("x", 20000)
	text = strings.Join(CollectReminders(&ReminderContext{AgentsMdUpdate: long}), "\n")
	if !strings.Contains(text, "truncated") {
		t.Fatalf("agents_md_changed missing truncation marker for long content")
	}
	if len(text) > 12000 {
		t.Fatalf("agents_md_changed content not truncated: len=%d", len(text))
	}

	// Neither update nor removal: silent.
	if msgs := CollectReminders(&ReminderContext{}); len(msgs) != 0 {
		t.Fatalf("agents_md_changed fired on empty context: %v", msgs)
	}
}
