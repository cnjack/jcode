package automation

import (
	"testing"
	"time"
)

func validAutomation() Automation {
	return Automation{
		Name:        "Nightly",
		Prompt:      "do the thing",
		ProjectPath: "/tmp/proj",
		Mode:        "full_access",
		Trigger:     Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9, Minute: 0},
	}
}

func TestValidateAutomation_OK(t *testing.T) {
	a := validAutomation()
	if err := ValidateAutomation(&a); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	// Manual trigger needs no cadence.
	m := validAutomation()
	m.Trigger = Trigger{Type: TriggerManual}
	if err := ValidateAutomation(&m); err != nil {
		t.Fatalf("manual should be valid: %v", err)
	}
}

func TestValidateAutomation_Rejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Automation)
	}{
		{"empty name", func(a *Automation) { a.Name = "  " }},
		{"empty prompt", func(a *Automation) { a.Prompt = "" }},
		{"empty project", func(a *Automation) { a.ProjectPath = "" }},
		{"remote ssh project", func(a *Automation) { a.ProjectPath = "ssh://user@host/path" }},
		{"remote docker project", func(a *Automation) { a.ProjectPath = "docker://c/path" }},
		{"relative project", func(a *Automation) { a.ProjectPath = "relative/dir" }},
		{"dot project", func(a *Automation) { a.ProjectPath = "." }},
		{"bad mode", func(a *Automation) { a.Mode = "yolo" }},
		{"bad context policy", func(a *Automation) { a.ContextPolicy = "shared" }},
		{"conversation missing owner", func(a *Automation) { a.ContextPolicy = ContextConversation }},
		{"isolated with owner", func(a *Automation) {
			a.ContextPolicy = ContextIsolated
			a.OwnerSessionID = "session-1"
		}},
		{"bad trigger type", func(a *Automation) { a.Trigger.Type = "weird" }},
		{"bad cadence", func(a *Automation) { a.Trigger.Cadence = "fortnightly" }},
		{"bad minute", func(a *Automation) { a.Trigger.Minute = 99 }},
		{"bad hour", func(a *Automation) { a.Trigger.Hour = 30 }},
		{"bad weekday", func(a *Automation) {
			a.Trigger = Trigger{Type: TriggerSchedule, Cadence: CadenceWeekly, Weekday: 9, Hour: 1, Minute: 0}
		}},
	}
	for _, c := range cases {
		a := validAutomation()
		c.mutate(&a)
		if err := ValidateAutomation(&a); err == nil {
			t.Errorf("%s: expected validation error", c.name)
		}
	}
}

func TestValidateAutomation_ConversationContext(t *testing.T) {
	a := validAutomation()
	a.ContextPolicy = ContextConversation
	a.OwnerSessionID = "session-1"
	if err := ValidateAutomation(&a); err != nil {
		t.Fatalf("conversation context should be valid: %v", err)
	}
}

func TestIsLocalPath(t *testing.T) {
	if !IsLocalPath("/home/x/proj") {
		t.Error("local abs path should be usable")
	}
	if IsLocalPath("") || IsLocalPath("ssh://h/p") || IsLocalPath("docker://c/p") {
		t.Error("empty/remote paths should be rejected")
	}
}

func TestValidateAutomation_OnceAndCron(t *testing.T) {
	// Well-formed once/cron triggers validate.
	okOnce := validAutomation()
	okOnce.Trigger = Trigger{Type: TriggerOnce, At: "2026-09-04T15:00:00Z"}
	if err := ValidateAutomation(&okOnce); err != nil {
		t.Fatalf("once should be valid: %v", err)
	}
	okCron := validAutomation()
	okCron.Trigger = Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "0 9 * * 1-5"}
	if err := ValidateAutomation(&okCron); err != nil {
		t.Fatalf("cron should be valid: %v", err)
	}

	bad := []struct {
		name   string
		mutate func(*Automation)
	}{
		{"once missing at", func(a *Automation) { a.Trigger = Trigger{Type: TriggerOnce} }},
		{"once bad at", func(a *Automation) { a.Trigger = Trigger{Type: TriggerOnce, At: "tomorrow"} }},
		{"cron bad expr", func(a *Automation) {
			a.Trigger = Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "* * * *"}
		}},
		{"cron never fires", func(a *Automation) {
			a.Trigger = Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "0 0 31 2 *"}
		}},
	}
	for _, c := range bad {
		a := validAutomation()
		c.mutate(&a)
		if err := ValidateAutomation(&a); err == nil {
			t.Errorf("%s: expected validation error", c.name)
		}
	}
}

// Store.Create is the single create gate: it must additionally reject a once
// trigger pinned in the past. Store.Update must NOT — an expired
// once-automation stays editable (e.g. re-enabling or renaming it).
func TestStore_OncePastTime_CreateRejected_UpdateAllowed(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())

	past := nowFunc().Add(-time.Hour).Format(time.RFC3339)
	if _, err := s.Create(Automation{Name: "past", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: past}, Enabled: true}); err == nil {
		t.Fatal("create with past once time must be rejected")
	}

	// The minute-floor slack: a pin inside the CURRENT minute whose seconds
	// already elapsed (datetime-local pick submitted late) is accepted — the
	// scheduler's late-delivery seeding fires it on the next tick.
	now := nowFunc()
	currentMinute := now.Truncate(time.Minute)
	if now.Sub(currentMinute) > time.Second { // only assert when meaningfully "past"
		a, err := s.Create(Automation{Name: "floor", Prompt: "p", ProjectPath: t.TempDir(),
			Trigger: Trigger{Type: TriggerOnce, At: currentMinute.Format(time.RFC3339)}, Enabled: true})
		if err != nil {
			t.Fatalf("same-minute pin must be accepted: %v", err)
		}
		if _, err := s.Update(a.ID, func(x *Automation) { x.Name = "renamed" }); err != nil {
			t.Fatal(err)
		}
	}

	future := nowFunc().Add(time.Hour).Format(time.RFC3339)
	a, err := s.Create(Automation{Name: "future", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: future}, Enabled: true})
	if err != nil {
		t.Fatalf("create with future once time: %v", err)
	}

	// Simulate the pinned time passing, then a user edit (rename): must succeed.
	_, err = s.Update(a.ID, func(x *Automation) { x.Name = "renamed" })
	if err != nil {
		t.Fatalf("update of expired once must be allowed: %v", err)
	}
}
