package automation

import "testing"

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
		{"bad mode", func(a *Automation) { a.Mode = "yolo" }},
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

func TestIsLocalPath(t *testing.T) {
	if !IsLocalPath("/home/x/proj") {
		t.Error("local abs path should be usable")
	}
	if IsLocalPath("") || IsLocalPath("ssh://h/p") || IsLocalPath("docker://c/p") {
		t.Error("empty/remote paths should be rejected")
	}
}
