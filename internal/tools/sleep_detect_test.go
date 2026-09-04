package tools

import (
	"strings"
	"testing"
)

func TestSleepDetect_LongSleepBlocked(t *testing.T) {
	// X-19: sleep 60 blocked
	blocked, reason := detectSleep("sleep 60")
	if !blocked {
		t.Fatal("expected sleep 60 to be blocked")
	}
	if reason == "" {
		t.Fatal("expected a reason message")
	}
	for _, want := range []string{"Do not split", "polling loop", "automation_create"} {
		if !strings.Contains(reason, want) {
			t.Errorf("block reason missing %q: %q", want, reason)
		}
	}
}

func TestSleepDetect_ShortSleepPasses(t *testing.T) {
	// X-20: sleep 5 passes
	blocked, _ := detectSleep("sleep 5")
	if blocked {
		t.Fatal("sleep 5 should not be blocked")
	}
}

func TestSleepDetect_PipeDetection(t *testing.T) {
	// X-21: sleep 60 && echo done blocked
	blocked, _ := detectSleep("sleep 60 && echo done")
	if !blocked {
		t.Fatal("sleep 60 in pipe should be blocked")
	}
}

func TestSleepDetect_MinutesBlocked(t *testing.T) {
	// X-22: sleep 5m blocked (5 minutes = 300s)
	blocked, _ := detectSleep("sleep 5m")
	if !blocked {
		t.Fatal("sleep 5m should be blocked")
	}
}

func TestSleepDetect_HoursBlocked(t *testing.T) {
	blocked, _ := detectSleep("sleep 1h")
	if !blocked {
		t.Fatal("sleep 1h should be blocked")
	}
}

func TestSleepDetect_ThresholdExact(t *testing.T) {
	// Exactly 30s should pass (not > 30)
	blocked, _ := detectSleep("sleep 30")
	if blocked {
		t.Fatal("sleep 30 should not be blocked (threshold is >30)")
	}
}

func TestSleepDetect_ThresholdJustOver(t *testing.T) {
	blocked, _ := detectSleep("sleep 31")
	if !blocked {
		t.Fatal("sleep 31 should be blocked")
	}
}

func TestSleepDetect_NoSleep(t *testing.T) {
	blocked, _ := detectSleep("echo hello && ls -la")
	if blocked {
		t.Fatal("command without sleep should not be blocked")
	}
}

func TestSleepDetect_SleepWithSuffix(t *testing.T) {
	blocked, _ := detectSleep("sleep 5s")
	if blocked {
		t.Fatal("sleep 5s should not be blocked")
	}
}

func TestSleepDetect_SleepInPipeline(t *testing.T) {
	blocked, _ := detectSleep("echo start | sleep 120 | echo end")
	if !blocked {
		t.Fatal("sleep 120 in pipeline should be blocked")
	}
}

func TestSleepDetect_SleepWithSemicolon(t *testing.T) {
	blocked, _ := detectSleep("echo start; sleep 60; echo end")
	if !blocked {
		t.Fatal("sleep 60 after semicolon should be blocked")
	}
}

func TestSleepDetect_SleepInOr(t *testing.T) {
	blocked, _ := detectSleep("false || sleep 120")
	if !blocked {
		t.Fatal("sleep 120 after || should be blocked")
	}
}

func TestSleepDetect_OneMinuteBlocked(t *testing.T) {
	blocked, _ := detectSleep("sleep 1m")
	if !blocked {
		t.Fatal("sleep 1m (60s) should be blocked")
	}
}
