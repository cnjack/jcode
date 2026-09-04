package automation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file implements 5-field cron expression parsing and next-fire
// computation for the "cron" cadence, in the host's local timezone. It is
// self-contained (no external cron library) and follows standard Vixie/POSIX
// behaviour:
//
//   - Fields: minute hour day-of-month month day-of-week.
//   - Per field: `*`, integers, ranges (a-b), lists (a,b,c), and steps
//     (`*/n` or `a-b/n`).
//   - Day-of-week accepts 0..7 with 7 folded to 0 (Sunday).
//   - When BOTH day-of-month and day-of-week are restricted they combine
//     with cron's OR rule; a bare `*` counts as unrestricted while `*/n`
//     counts as a restriction.
//
// Termination: computing `next` for a legal-but-never-fires expression like
// `0 0 31 2 *` must not spin. The scan is bounded at cronMaxSearchYears and
// returns ok=false past that — the same signal validation uses to reject
// such expressions at create/update time.

const (
	cronMaxSearchYears = 5
	cronMaxExprLen     = 100
)

// CronExpr is a parsed cron expression. Opaque; pass it back to NextCronFire.
type CronExpr struct {
	raw         string
	minutes     map[int]bool
	hours       map[int]bool
	daysOfMonth map[int]bool
	months      map[int]bool
	daysOfWeek  map[int]bool
	// sortedHours/sortedMinutes are the same values in ascending order, so
	// NextCronFire's day scan can return the EARLIEST match — Go map range
	// order is randomized and would return an arbitrary matching slot.
	sortedHours   []int
	sortedMinutes []int
	// domWildcard/dowWildcard record whether the source field was a bare `*`
	// — needed so the dom/dow OR rule applies only when both are restricted.
	domWildcard bool
	dowWildcard bool
}

// ParseCronExpr parses a 5-field cron expression. The returned error names the
// offending field so agent-facing validation messages are actionable.
func ParseCronExpr(expr string) (*CronExpr, error) {
	if len(expr) > cronMaxExprLen {
		return nil, fmt.Errorf("cron expression too long (max %d chars)", cronMaxExprLen)
	}
	trimmed := strings.TrimSpace(expr)
	fields := strings.Fields(trimmed)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have exactly 5 fields (minute hour day-of-month month day-of-week); got %d", len(fields))
	}
	minutes, err := parseCronField(fields[0], 0, 59, "minute")
	if err != nil {
		return nil, err
	}
	hours, err := parseCronField(fields[1], 0, 23, "hour")
	if err != nil {
		return nil, err
	}
	dom, err := parseCronField(fields[2], 1, 31, "day-of-month")
	if err != nil {
		return nil, err
	}
	months, err := parseCronField(fields[3], 1, 12, "month")
	if err != nil {
		return nil, err
	}
	dowRaw, err := parseCronField(fields[4], 0, 7, "day-of-week")
	if err != nil {
		return nil, err
	}
	dow := make(map[int]bool, len(dowRaw))
	for v := range dowRaw {
		if v == 7 {
			v = 0
		}
		dow[v] = true
	}
	return &CronExpr{
		raw:           trimmed,
		minutes:       minutes,
		hours:         hours,
		sortedHours:   sortedValues(hours),
		sortedMinutes: sortedValues(minutes),
		daysOfMonth:   dom,
		months:        months,
		daysOfWeek:    dow,
		domWildcard:   fields[2] == "*",
		dowWildcard:   fields[4] == "*",
	}, nil
}

// String returns the normalized (whitespace-collapsed) expression.
func (c *CronExpr) String() string { return c.raw }

// dayMatches applies cron's dom/dow combination rule: if either field is a
// bare wildcard the day must satisfy the restricted field; if both are
// restricted the day matches when EITHER does.
func (c *CronExpr) dayMatches(t time.Time) bool {
	domOK := c.daysOfMonth[t.Day()]
	dowOK := c.daysOfWeek[int(t.Weekday())]
	switch {
	case c.domWildcard && c.dowWildcard:
		return true
	case c.domWildcard:
		return dowOK
	case c.dowWildcard:
		return domOK
	default:
		return domOK || dowOK
	}
}

// NextCronFire returns the next instant strictly after `after` that matches
// the expression, evaluated in after's own location. ok=false when no match
// exists within the search window (legal-but-never-fires expression).
//
// The scan walks day-by-day and builds each candidate with time.Date, which
// normalizes non-existent wall-clock times on spring-forward days (a 02:30
// fire lands on the normalized real instant). Fall-back duplicates are
// deduped by the caller via SlotKey/LastFiredSlot, not here.
func NextCronFire(after time.Time, c *CronExpr) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	loc := after.Location()

	// First candidate day starts at `after`'s own calendar day so a fire
	// later the same day is found.
	day := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, loc)
	end := day.AddDate(cronMaxSearchYears, 0, 0)
	for day.Before(end) {
		if c.months[int(day.Month())] && c.dayMatches(day) {
			for _, h := range c.sortedHours {
				for _, m := range c.sortedMinutes {
					cand := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
					if cand.After(after) {
						return cand, true
					}
				}
			}
		}
		day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
	}
	return time.Time{}, false
}

func sortedValues(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// parseCronField parses one cron field into a set of allowed values.
// Values are digit-only — `Number()`-style acceptance of `1e1`/`0x10`/`+5`
// would silently reschedule instead of surfacing the typo as a parse error.
func parseCronField(field string, min, max int, name string) (map[int]bool, error) {
	out := make(map[int]bool)
	if field == "" {
		return nil, fmt.Errorf("cron %s field is empty", name)
	}
	for _, term := range strings.Split(field, ",") {
		if term == "" {
			return nil, fmt.Errorf("cron %s field has empty term in list", name)
		}
		lo, hi, step := min, max, 1
		base, stepPart, hasStep := strings.Cut(term, "/")
		if hasStep {
			n, err := strconv.Atoi(stepPart)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cron %s field has invalid step %q", name, stepPart)
			}
			step = n
		}
		switch {
		case base == "*":
			// full range with optional step
		case strings.Contains(base, "-"):
			bounds := strings.SplitN(base, "-", 2)
			// A step on a range uses `a-b/n`; the step was cut above, so
			// bounds must both be plain digits here.
			start, err1 := parseCronValue(bounds[0], name)
			stop, err2 := parseCronValue(bounds[1], name)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("cron %s field has invalid range %q", name, base)
			}
			if start > stop {
				return nil, fmt.Errorf("cron %s field range %q is reversed", name, base)
			}
			lo, hi = start, stop
		default:
			v, err := parseCronValue(base, name)
			if err != nil {
				return nil, err
			}
			if v < min || v > max {
				return nil, fmt.Errorf("cron %s field value %d out of range %d-%d", name, v, min, max)
			}
			if hasStep {
				// `5/10` (value with step) — non-standard; treat as start-at
				// with max ceiling like Vixie cron does.
				lo, hi = v, max
			} else {
				out[v] = true
				continue
			}
		}
		for v := lo; v <= hi; v += step {
			if v < min || v > max {
				return nil, fmt.Errorf("cron %s field value %d out of range %d-%d", name, v, min, max)
			}
			out[v] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cron %s field matches no values", name)
	}
	return out, nil
}

func parseCronValue(s, name string) (int, error) {
	if s == "" || !isAllDigits(s) {
		return 0, fmt.Errorf("cron %s field has invalid value %q", name, s)
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("cron %s field has invalid value %q", name, s)
	}
	return v, nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
