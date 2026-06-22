package usage

import (
	"sort"
	"time"
)

// dateLayout is the canonical local date format used for event bucketing.
const dateLayout = "2006-01-02"

// Today returns the current local date as YYYY-MM-DD.
func Today() string { return time.Now().Format(dateLayout) }

// Totals holds cumulative token counters over the aggregated window.
type Totals struct {
	Total      int64 `json:"total"`
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	Cached     int64 `json:"cached"`
	Reasoning  int64 `json:"reasoning"`
	CacheWrite int64 `json:"cache_write"`
	Calls      int64 `json:"calls"`
	Turns      int64 `json:"turns"` // number of recorded events
}

// DayBucket is one day's rolled-up usage.
type DayBucket struct {
	Date   string `json:"date"`
	Tokens int64  `json:"tokens"`
	Turns  int64  `json:"turns"` // recorded turns ("轮") that day
	Calls  int64  `json:"calls"`
}

// Share is a labelled token total (per-model or per-project).
type Share struct {
	Name   string  `json:"name"`
	Tokens int64   `json:"tokens"`
	Share  float64 `json:"share"` // fraction of the grand total, 0-1
}

// Aggregated is the full derived view over a set of events.
type Aggregated struct {
	Totals         Totals
	ActiveDays     int
	CurrentStreak  int
	LongestStreak  int
	MostUsedModel  string
	CacheHitRate   float64 // cached / prompt, clamped to [0,1]
	CacheSupported bool
	Days           map[string]*DayBucket
	ByModel        []Share
	ByProject      []Share
}

// Aggregate reduces raw events into derived statistics. `today` (YYYY-MM-DD,
// local) anchors the current-streak computation so the function stays pure and
// testable.
func Aggregate(events []Event, today string) Aggregated {
	agg := Aggregated{Days: make(map[string]*DayBucket)}
	byModel := map[string]int64{}
	byProject := map[string]int64{}
	anyCacheSeen := false

	for _, ev := range events {
		if ev.CacheSeen {
			anyCacheSeen = true
		}
		agg.Totals.Total += int64(ev.Total)
		agg.Totals.Prompt += int64(ev.Prompt)
		agg.Totals.Completion += int64(ev.Completion)
		agg.Totals.Cached += int64(ev.Cached)
		agg.Totals.Reasoning += int64(ev.Reasoning)
		agg.Totals.CacheWrite += int64(ev.CacheWrite)
		agg.Totals.Calls += int64(ev.Calls)
		agg.Totals.Turns++

		d := agg.Days[ev.Date]
		if d == nil {
			d = &DayBucket{Date: ev.Date}
			agg.Days[ev.Date] = d
		}
		d.Tokens += int64(ev.Total)
		d.Turns++
		d.Calls += int64(ev.Calls)

		if ev.Model != "" {
			byModel[ev.Model] += int64(ev.Total)
		}
		if ev.Project != "" {
			byProject[ev.Project] += int64(ev.Total)
		}
	}

	agg.ActiveDays = len(agg.Days)
	agg.CurrentStreak = currentStreak(agg.Days, today)
	agg.LongestStreak = longestStreak(agg.Days)
	// A turn that reported cache details (CacheSeen) means the provider supports
	// caching even if no tokens were served from cache; the Cached>0 fallback
	// keeps older events (written before CacheSeen existed) correct.
	agg.CacheSupported = anyCacheSeen || agg.Totals.Cached > 0
	if agg.Totals.Prompt > 0 {
		r := float64(agg.Totals.Cached) / float64(agg.Totals.Prompt)
		switch {
		case r < 0:
			r = 0
		case r > 1:
			r = 1
		}
		agg.CacheHitRate = r
	}
	agg.ByModel = toShares(byModel, agg.Totals.Total)
	agg.ByProject = toShares(byProject, agg.Totals.Total)
	if len(agg.ByModel) > 0 {
		agg.MostUsedModel = agg.ByModel[0].Name
	}
	return agg
}

// Trend returns the day buckets in ascending date order.
func (a Aggregated) Trend() []DayBucket {
	out := make([]DayBucket, 0, len(a.Days))
	for _, d := range a.Days {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// toShares sorts a label→tokens map into descending Shares.
func toShares(m map[string]int64, grand int64) []Share {
	out := make([]Share, 0, len(m))
	for name, tok := range m {
		s := Share{Name: name, Tokens: tok}
		if grand > 0 {
			s.Share = float64(tok) / float64(grand)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// currentStreak counts consecutive active days ending at `today`, or at
// yesterday if today has no activity yet (so a streak isn't considered broken
// before the user has worked today). Returns 0 if the most recent activity is
// older than yesterday.
func currentStreak(days map[string]*DayBucket, today string) int {
	if len(days) == 0 {
		return 0
	}
	cur, err := time.Parse(dateLayout, today)
	if err != nil {
		return 0
	}
	if !active(days, cur) {
		cur = cur.AddDate(0, 0, -1)
	}
	streak := 0
	for active(days, cur) {
		streak++
		cur = cur.AddDate(0, 0, -1)
	}
	return streak
}

// longestStreak finds the longest run of consecutive calendar days with
// activity.
func longestStreak(days map[string]*DayBucket) int {
	if len(days) == 0 {
		return 0
	}
	dates := make([]time.Time, 0, len(days))
	for d := range days {
		t, err := time.Parse(dateLayout, d)
		if err != nil {
			continue
		}
		dates = append(dates, t)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	best, run := 1, 1
	for i := 1; i < len(dates); i++ {
		if dates[i].Equal(dates[i-1].AddDate(0, 0, 1)) {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
	}
	return best
}

func active(days map[string]*DayBucket, t time.Time) bool {
	d, ok := days[t.Format(dateLayout)]
	return ok && d.Tokens > 0
}
