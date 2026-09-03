package automation

import (
	"fmt"
	"time"
)

// ComputeNextRun returns the next instant strictly after `after` that matches
// the trigger, evaluated in after's own location (the host's local tz). It is a
// pure function (no time.Now) so it is fully unit-testable, including DST
// transitions — see schedule_test.go.
//
// DST handling: each candidate is built with time.Date, which normalizes
// non-existent wall-clock times (spring-forward, e.g. a 02:30 daily on the day
// the clock jumps 02:00→03:00 lands on a real instant). Fall-back (a slot that
// occurs twice) is deduped by the caller via SlotKey/LastFiredSlot, not here.
//
// Returns ok=false for non-schedule triggers (manual never auto-fires) and for
// a once trigger whose pinned time is not strictly in the future.
func ComputeNextRun(after time.Time, t Trigger) (time.Time, bool) {
	switch t.Type {
	case TriggerOnce:
		at, err := time.Parse(time.RFC3339, t.At)
		if err != nil || !at.After(after) {
			return time.Time{}, false
		}
		return at, true

	case TriggerSchedule:
		loc := after.Location()
		switch t.Cadence {
		case CadenceHourly:
			// Next occurrence of :MM strictly after `after`.
			c := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), t.Minute, 0, 0, loc)
			for !c.After(after) {
				c = c.Add(time.Hour)
			}
			return c, true

		case CadenceDaily:
			c := time.Date(after.Year(), after.Month(), after.Day(), t.Hour, t.Minute, 0, 0, loc)
			for !c.After(after) {
				c = time.Date(c.Year(), c.Month(), c.Day()+1, t.Hour, t.Minute, 0, 0, loc)
			}
			return c, true

		case CadenceWeekly:
			c := time.Date(after.Year(), after.Month(), after.Day(), t.Hour, t.Minute, 0, 0, loc)
			// Advance day-by-day (calendar-safe) until weekday matches and it's in the future.
			for i := 0; i < 8; i++ {
				if int(c.Weekday()) == t.Weekday && c.After(after) {
					return c, true
				}
				c = time.Date(c.Year(), c.Month(), c.Day()+1, t.Hour, t.Minute, 0, 0, loc)
			}
			return c, true // unreachable in practice; loop always finds a match within 7 days

		case CadenceCron:
			expr, err := ParseCronExpr(t.Expr)
			if err != nil {
				return time.Time{}, false
			}
			return NextCronFire(after, expr)

		default:
			return time.Time{}, false
		}

	default:
		return time.Time{}, false
	}
}

// SlotKey is a stable dedup key for a fire instant: the local calendar minute.
// Two fires at the same wall-clock minute (e.g. a DST fall-back repeat) share a
// key, so LastFiredSlot can suppress the duplicate.
func SlotKey(t time.Time) string {
	return t.Format("2006-01-02T15:04")
}

// HumanSchedule renders a trigger for display, e.g. "Daily at 09:00",
// "Weekly on Mon at 14:30", "Hourly at :05", "Manual",
// "Once at 2026-09-04 15:00", or `Cron "*/15 * * * *"`.
func HumanSchedule(t Trigger) string {
	switch t.Type {
	case TriggerManual:
		return "Manual"
	case TriggerOnce:
		if at, err := time.Parse(time.RFC3339, t.At); err == nil {
			return fmt.Sprintf("Once at %s", at.Format("2006-01-02 15:04"))
		}
		return "Once"
	}
	switch t.Cadence {
	case CadenceHourly:
		return fmt.Sprintf("Hourly at :%02d", t.Minute)
	case CadenceDaily:
		return fmt.Sprintf("Daily at %02d:%02d", t.Hour, t.Minute)
	case CadenceWeekly:
		return fmt.Sprintf("Weekly on %s at %02d:%02d", weekdayName(t.Weekday), t.Hour, t.Minute)
	case CadenceCron:
		return fmt.Sprintf("Cron %q", t.Expr)
	default:
		return string(t.Cadence)
	}
}

// Badge renders a short cadence label for cards: "Daily" / "Weekly" / "Hourly"
// / "Manual" / "Once" / "Cron".
func Badge(t Trigger) string {
	switch t.Type {
	case TriggerManual:
		return "Manual"
	case TriggerOnce:
		return "Once"
	}
	switch t.Cadence {
	case CadenceHourly:
		return "Hourly"
	case CadenceDaily:
		return "Daily"
	case CadenceWeekly:
		return "Weekly"
	case CadenceCron:
		return "Cron"
	default:
		return string(t.Cadence)
	}
}

func weekdayName(w int) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	if w < 0 || w > 6 {
		return "?"
	}
	return names[w]
}
