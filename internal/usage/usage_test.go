package usage

import (
	"path/filepath"
	"testing"
)

func ev(date, model, project string, total, prompt, cached int) Event {
	return Event{Date: date, Model: model, Project: project, Total: total, Prompt: prompt, Cached: cached, Completion: total - prompt, Calls: 1}
}

func TestStore_RecordAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	s := NewStore(path)

	if err := s.Record(ev("2026-06-20", "glm-5.2", "/p", 100, 80, 60)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := s.Record(ev("2026-06-21", "glm-5.2", "/p", 200, 150, 120)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Empty turn is dropped.
	if err := s.Record(Event{Date: "2026-06-21"}); err != nil {
		t.Fatalf("Record empty: %v", err)
	}

	all, err := s.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("Load() returned %d events, want 2 (empty turn should be dropped)", len(all))
	}

	since, err := s.Load("2026-06-21")
	if err != nil {
		t.Fatalf("Load since: %v", err)
	}
	if len(since) != 1 || since[0].Total != 200 {
		t.Fatalf("Load(since) = %+v, want 1 event with total 200", since)
	}
}

func TestStore_LoadMissingFile(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nope.jsonl"))
	got, err := s.Load("")
	if err != nil || got != nil {
		t.Fatalf("Load missing = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestAggregate_Totals(t *testing.T) {
	events := []Event{
		ev("2026-06-20", "glm-5.2", "/a", 1000, 800, 600),
		ev("2026-06-21", "glm-5.2", "/a", 2000, 1600, 1400),
		ev("2026-06-21", "claude", "/b", 500, 400, 0),
	}
	a := Aggregate(events, "2026-06-21")

	if a.Totals.Total != 3500 {
		t.Errorf("Total = %d, want 3500", a.Totals.Total)
	}
	if a.Totals.Turns != 3 {
		t.Errorf("Turns = %d, want 3", a.Totals.Turns)
	}
	if a.ActiveDays != 2 {
		t.Errorf("ActiveDays = %d, want 2", a.ActiveDays)
	}
	// cached/prompt = (600+1400+0)/(800+1600+400) = 2000/2800
	if got := a.CacheHitRate; got < 0.714 || got > 0.715 {
		t.Errorf("CacheHitRate = %v, want ~0.7143", got)
	}
	if !a.CacheSupported {
		t.Error("CacheSupported = false, want true")
	}
	// glm-5.2 = 3000, claude = 500 → glm most used.
	if a.MostUsedModel != "glm-5.2" {
		t.Errorf("MostUsedModel = %q, want glm-5.2", a.MostUsedModel)
	}
	if len(a.ByModel) != 2 || a.ByModel[0].Name != "glm-5.2" || a.ByModel[0].Tokens != 3000 {
		t.Errorf("ByModel = %+v, want glm-5.2 first with 3000", a.ByModel)
	}
	if len(a.ByProject) != 2 || a.ByProject[0].Name != "/a" || a.ByProject[0].Tokens != 3000 {
		t.Errorf("ByProject = %+v, want /a first with 3000", a.ByProject)
	}
	// Day buckets.
	if d := a.Days["2026-06-21"]; d == nil || d.Tokens != 2500 || d.Turns != 2 {
		t.Errorf("Days[2026-06-21] = %+v, want tokens 2500 turns 2", d)
	}
}

func TestAggregate_Streaks(t *testing.T) {
	tests := []struct {
		name       string
		dates      []string
		today      string
		wantCur    int
		wantLong   int
		wantActive int
	}{
		{"empty", nil, "2026-06-21", 0, 0, 0},
		{"single today", []string{"2026-06-21"}, "2026-06-21", 1, 1, 1},
		{
			"three in a row ending today",
			[]string{"2026-06-19", "2026-06-20", "2026-06-21"},
			"2026-06-21", 3, 3, 3,
		},
		{
			"ends yesterday, today empty still counts",
			[]string{"2026-06-19", "2026-06-20"},
			"2026-06-21", 2, 2, 2,
		},
		{
			"gap breaks current streak",
			[]string{"2026-06-10", "2026-06-20", "2026-06-21"},
			"2026-06-21", 2, 2, 3,
		},
		{
			"stale: last activity 3 days ago",
			[]string{"2026-06-17", "2026-06-18"},
			"2026-06-21", 0, 2, 2,
		},
		{
			"longest in the middle",
			[]string{"2026-06-01", "2026-06-02", "2026-06-03", "2026-06-10"},
			"2026-06-21", 0, 3, 4,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var events []Event
			for _, d := range tc.dates {
				events = append(events, ev(d, "m", "/p", 100, 80, 40))
			}
			a := Aggregate(events, tc.today)
			if a.CurrentStreak != tc.wantCur {
				t.Errorf("CurrentStreak = %d, want %d", a.CurrentStreak, tc.wantCur)
			}
			if a.LongestStreak != tc.wantLong {
				t.Errorf("LongestStreak = %d, want %d", a.LongestStreak, tc.wantLong)
			}
			if a.ActiveDays != tc.wantActive {
				t.Errorf("ActiveDays = %d, want %d", a.ActiveDays, tc.wantActive)
			}
		})
	}
}

func TestAggregate_NoCacheSupport(t *testing.T) {
	a := Aggregate([]Event{ev("2026-06-21", "m", "/p", 100, 80, 0)}, "2026-06-21")
	if a.CacheSupported {
		t.Error("CacheSupported = true, want false when no cached tokens seen")
	}
	if a.CacheHitRate != 0 {
		t.Errorf("CacheHitRate = %v, want 0", a.CacheHitRate)
	}
}

func TestAggregate_CacheSeenZeroHit(t *testing.T) {
	// Provider reported cache details but served 0 cached tokens: caching IS
	// supported, so the stats page should not claim it isn't.
	e := ev("2026-06-21", "m", "/p", 100, 80, 0)
	e.CacheSeen = true
	a := Aggregate([]Event{e}, "2026-06-21")
	if !a.CacheSupported {
		t.Error("CacheSupported = false, want true when CacheSeen even with 0 cached")
	}
	if a.CacheHitRate != 0 {
		t.Errorf("CacheHitRate = %v, want 0", a.CacheHitRate)
	}
}

func TestAggregate_Trend(t *testing.T) {
	a := Aggregate([]Event{
		ev("2026-06-21", "m", "/p", 100, 80, 40),
		ev("2026-06-19", "m", "/p", 100, 80, 40),
		ev("2026-06-20", "m", "/p", 100, 80, 40),
	}, "2026-06-21")
	trend := a.Trend()
	if len(trend) != 3 || trend[0].Date != "2026-06-19" || trend[2].Date != "2026-06-21" {
		t.Errorf("Trend() not ascending: %+v", trend)
	}
}
