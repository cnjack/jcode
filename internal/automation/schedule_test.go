package automation

import (
	"testing"
	"time"
)

func mustLoadNewYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York timezone unavailable: %v", err)
	}
	return loc
}

func TestComputeNextRun_Hourly(t *testing.T) {
	after := time.Date(2026, 6, 23, 10, 12, 0, 0, time.UTC)
	got, ok := ComputeNextRun(after, Trigger{Type: TriggerSchedule, Cadence: CadenceHourly, Minute: 5})
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 6, 23, 11, 5, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("hourly: got %v want %v", got, want)
	}
	// When the minute is still ahead this hour, it stays in the same hour.
	got2, _ := ComputeNextRun(time.Date(2026, 6, 23, 10, 2, 0, 0, time.UTC),
		Trigger{Type: TriggerSchedule, Cadence: CadenceHourly, Minute: 5})
	if want2 := time.Date(2026, 6, 23, 10, 5, 0, 0, time.UTC); !got2.Equal(want2) {
		t.Fatalf("hourly same-hour: got %v want %v", got2, want2)
	}
}

func TestComputeNextRun_Daily_StrictlyAfter(t *testing.T) {
	// Exactly at the slot → must roll to tomorrow (strictly after).
	after := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	got, _ := ComputeNextRun(after, Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9, Minute: 0})
	want := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("daily strict: got %v want %v", got, want)
	}
}

func TestComputeNextRun_Weekly(t *testing.T) {
	// 2026-06-23 is a Tuesday(2). Want Friday(5) 17:00.
	after := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	got, _ := ComputeNextRun(after, Trigger{Type: TriggerSchedule, Cadence: CadenceWeekly, Weekday: 5, Hour: 17, Minute: 0})
	if got.Weekday() != time.Friday {
		t.Fatalf("weekly: got weekday %v", got.Weekday())
	}
	if want := time.Date(2026, 6, 26, 17, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("weekly: got %v want %v", got, want)
	}
}

func TestComputeNextRun_Manual(t *testing.T) {
	if _, ok := ComputeNextRun(time.Now(), Trigger{Type: TriggerManual}); ok {
		t.Fatal("manual trigger must not produce a next run")
	}
}

func TestComputeNextRun_DST_SpringForward(t *testing.T) {
	loc := mustLoadNewYork(t)
	// 2026-03-08: clocks jump 02:00 -> 03:00. A daily 02:30 does not exist that
	// day; ComputeNextRun must still return a real future instant (normalized).
	after := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	got, ok := ComputeNextRun(after, Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 2, Minute: 30})
	if !ok {
		t.Fatal("expected ok")
	}
	if !got.After(after) {
		t.Fatalf("spring-forward: result %v not after %v", got, after)
	}
	// Round-trips to a valid instant (no zero / no panic) and is the same day.
	if got.Day() != 8 {
		t.Fatalf("spring-forward: expected same calendar day, got %v", got)
	}
}

func TestComputeNextRun_DST_FallBack_SlotDedup(t *testing.T) {
	loc := mustLoadNewYork(t)
	// During fall-back the 01:30 wall-clock occurs twice. SlotKey must be equal
	// for both so the scheduler's LastFiredSlot guard suppresses the second.
	first := time.Date(2026, 11, 1, 1, 30, 0, 0, loc)
	second := first.Add(time.Hour) // same wall clock, different offset
	if SlotKey(first) != SlotKey(second) {
		t.Fatalf("fall-back slot keys differ: %q vs %q", SlotKey(first), SlotKey(second))
	}
}

func TestHumanScheduleAndBadge(t *testing.T) {
	cases := []struct {
		tr    Trigger
		human string
		badge string
	}{
		{Trigger{Type: TriggerManual}, "Manual", "Manual"},
		{Trigger{Type: TriggerSchedule, Cadence: CadenceHourly, Minute: 5}, "Hourly at :05", "Hourly"},
		{Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9, Minute: 0}, "Daily at 09:00", "Daily"},
		{Trigger{Type: TriggerSchedule, Cadence: CadenceWeekly, Weekday: 1, Hour: 14, Minute: 30}, "Weekly on Mon at 14:30", "Weekly"},
	}
	for _, c := range cases {
		if got := HumanSchedule(c.tr); got != c.human {
			t.Errorf("HumanSchedule(%v)=%q want %q", c.tr, got, c.human)
		}
		if got := Badge(c.tr); got != c.badge {
			t.Errorf("Badge(%v)=%q want %q", c.tr, got, c.badge)
		}
	}
}
