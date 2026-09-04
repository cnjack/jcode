package automation

import (
	"testing"
	"time"
)

func mustParseCron(t *testing.T, expr string) *CronExpr {
	t.Helper()
	c, err := ParseCronExpr(expr)
	if err != nil {
		t.Fatalf("ParseCronExpr(%q): %v", expr, err)
	}
	return c
}

func TestParseCronExpr_FieldExpansion(t *testing.T) {
	cases := []struct {
		expr  string
		field int // 0=minute 1=hour 2=dom 3=month 4=dow
		want  []int
	}{
		{"*/15 * * * *", 0, []int{0, 15, 30, 45}},
		{"5 * * * *", 0, []int{5}},
		{"5/10 * * * *", 0, []int{5, 15, 25, 35, 45, 55}}, // value-with-step → start..max
		{"1-5 * * * *", 0, []int{1, 2, 3, 4, 5}},
		{"1-5/2 * * * *", 0, []int{1, 3, 5}},
		{"0 9,18 * * *", 1, []int{9, 18}},
		{"0 0 1,15 * *", 2, []int{1, 15}},
		{"0 0 * 2,12 *", 3, []int{2, 12}},
		{"0 0 * * 1-5", 4, []int{1, 2, 3, 4, 5}},
		{"0 0 * * 6,7", 4, []int{0, 6}}, // 7 folds to Sunday
	}
	for _, c := range cases {
		parsed := mustParseCron(t, c.expr)
		var got map[int]bool
		switch c.field {
		case 0:
			got = parsed.minutes
		case 1:
			got = parsed.hours
		case 2:
			got = parsed.daysOfMonth
		case 3:
			got = parsed.months
		case 4:
			got = parsed.daysOfWeek
		}
		if len(got) != len(c.want) {
			t.Fatalf("%q field %d: got %v want %v", c.expr, c.field, got, c.want)
		}
		for _, v := range c.want {
			if !got[v] {
				t.Fatalf("%q field %d: missing %d (got %v)", c.expr, c.field, v, got)
			}
		}
	}
}

func TestParseCronExpr_Errors(t *testing.T) {
	cases := []string{
		"",              // empty
		"* * * *",       // 4 fields
		"* * * * * *",   // 6 fields
		"60 * * * *",    // minute out of range
		"* 24 * * *",    // hour out of range
		"* * 32 * *",    // dom out of range
		"* * * 13 *",    // month out of range
		"* * * * 8",     // dow out of range
		"*/0 * * * *",   // zero step
		"1- * * * *",    // open range
		"5-1 * * * *",   // reversed range
		"0x10 * * * *",  // non-decimal value
		"1e1 * * * *",   // scientific notation
		"+5 * * * *",    // signed value
		"MON * * * *",   // names unsupported
		"@daily",        // macros unsupported
		"1,,2 * * * *",  // empty list term
		"1/2/3 * * * *", // garbage step
	}
	for _, expr := range cases {
		if _, err := ParseCronExpr(expr); err == nil {
			t.Errorf("ParseCronExpr(%q): expected error, got nil", expr)
		}
	}
}

func TestNextCronFire_Basic(t *testing.T) {
	// 17:12 → next */15 slot is 17:15.
	after := time.Date(2026, 6, 23, 17, 12, 0, 0, time.UTC)
	got, ok := NextCronFire(after, mustParseCron(t, "*/15 * * * *"))
	if !ok || !got.Equal(time.Date(2026, 6, 23, 17, 15, 0, 0, time.UTC)) {
		t.Fatalf("*/15 from 17:12: got %v ok=%v", got, ok)
	}

	// Strictly after: at exactly a match → next occurrence.
	got, _ = NextCronFire(time.Date(2026, 6, 23, 17, 15, 0, 0, time.UTC), mustParseCron(t, "*/15 * * * *"))
	if !got.Equal(time.Date(2026, 6, 23, 17, 30, 0, 0, time.UTC)) {
		t.Fatalf("*/15 from 17:15 (strict): got %v", got)
	}

	// Weekday set: Tue 2026-06-23 → next Mon-Fri 09:00 slot is Wed 2026-06-24.
	got, _ = NextCronFire(time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC), mustParseCron(t, "0 9 * * 1-5"))
	if !got.Equal(time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekdays 9am: got %v", got)
	}

	// Weekly-only: Fri 18:00 from Tue skips to Fri.
	got, _ = NextCronFire(time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC), mustParseCron(t, "0 18 * * 5"))
	if !got.Equal(time.Date(2026, 6, 26, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("friday 18: got %v", got)
	}

	// dom/dow OR rule: `0 0 1 * 1` fires on the 1st OR on Mondays.
	// 2026-06-23 (Tue) → next: Mon 2026-06-29 (dow hit), then Mon 07-01 also dom hit.
	got, _ = NextCronFire(time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC), mustParseCron(t, "0 0 1 * 1"))
	if !got.Equal(time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dom/dow OR: got %v", got)
	}

	// dom-restricted only: `0 0 1 * *` — 2026-06-23 → July 1.
	got, _ = NextCronFire(time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC), mustParseCron(t, "0 0 1 * *"))
	if !got.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dom only: got %v", got)
	}

	// Leap day: `0 0 29 2 *` from 2026 → 2028 (2028 is a leap year; 2027 is not).
	got, _ = NextCronFire(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), mustParseCron(t, "0 0 29 2 *"))
	if !got.Equal(time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("leap day: got %v", got)
	}
}

func TestNextCronFire_NeverFires(t *testing.T) {
	// Feb 31st never exists — no fire within the 5-year window.
	if _, ok := NextCronFire(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), mustParseCron(t, "0 0 31 2 *")); ok {
		t.Fatal("impossible date must not fire")
	}
	if _, ok := NextCronFire(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil); ok {
		t.Fatal("nil expr must not fire")
	}
}

func TestNextCronFire_Timezone(t *testing.T) {
	loc := mustLoadNewYork(t)
	// 09:00 New York on 2026-07-01 = 13:00 UTC. nextCron from 08:00 local
	// must land on 09:00 LOCAL, not a UTC wall-clock reading.
	after := time.Date(2026, 7, 1, 8, 0, 0, 0, loc)
	got, ok := NextCronFire(after, mustParseCron(t, "0 9 * * *"))
	if !ok {
		t.Fatal("expected ok")
	}
	if want := time.Date(2026, 7, 1, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("local 9am: got %v want %v", got, want)
	}
}

func TestNextCronFire_DST_Transitions(t *testing.T) {
	loc := mustLoadNewYork(t)

	// Spring forward 2026-03-08: 02:30 does not exist. Go's time.Date
	// interprets the missing wall clock in the POST-transition offset
	// (02:30 EDT = 06:30 UTC), yielding a real same-day instant. Pin it.
	after := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	got, ok := NextCronFire(after, mustParseCron(t, "30 2 * * *"))
	if !ok {
		t.Fatal("spring-forward: expected ok")
	}
	if want := time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("spring-forward: got %v want normalized %v", got, want)
	}

	// Fall back 2026-11-01: 01:30 occurs twice; time.Date picks the first.
	after = time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	got, ok = NextCronFire(after, mustParseCron(t, "30 1 * * *"))
	if !ok {
		t.Fatal("fall-back: expected ok")
	}
	if want := time.Date(2026, 11, 1, 1, 30, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("fall-back: got %v want %v", got, want)
	}
}

func TestComputeNextRun_Once(t *testing.T) {
	at := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)
	trig := Trigger{Type: TriggerOnce, At: at.Format(time.RFC3339)}

	got, ok := ComputeNextRun(at.Add(-time.Minute), trig)
	if !ok || !got.Equal(at) {
		t.Fatalf("once future: got %v ok=%v", got, ok)
	}

	// At exactly the pinned time (or after) there is no NEXT fire.
	if _, ok := ComputeNextRun(at, trig); ok {
		t.Fatal("once at exact time must not produce a next run")
	}
	if _, ok := ComputeNextRun(at.Add(time.Hour), trig); ok {
		t.Fatal("once in the past must not produce a next run")
	}

	if _, ok := ComputeNextRun(at, Trigger{Type: TriggerOnce, At: "not-a-time"}); ok {
		t.Fatal("unparseable once at-time must not produce a next run")
	}
}

func TestComputeNextRun_CronCadence(t *testing.T) {
	after := time.Date(2026, 6, 23, 10, 12, 0, 0, time.UTC)
	got, ok := ComputeNextRun(after, Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "*/15 * * * *"})
	if !ok || !got.Equal(time.Date(2026, 6, 23, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("cron cadence: got %v ok=%v", got, ok)
	}

	// Invalid expression never fires.
	if _, ok := ComputeNextRun(after, Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "bogus"}); ok {
		t.Fatal("invalid cron expr must not produce a next run")
	}
}

func TestHumanScheduleAndBadge_OnceAndCron(t *testing.T) {
	localAt := time.Date(2026, 9, 4, 15, 0, 0, 0, time.Local).Format(time.RFC3339)
	cases := []struct {
		tr    Trigger
		human string
		badge string
	}{
		// At renders in the host's local timezone regardless of stored offset.
		{Trigger{Type: TriggerOnce, At: localAt}, "Once at 2026-09-04 15:00", "Once"},
		{Trigger{Type: TriggerOnce}, "Once", "Once"},
		{Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "*/15 * * * *"}, `Cron "*/15 * * * *"`, "Cron"},
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
