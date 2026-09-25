package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.13.8", "0.13.8", 0, true},
		{"v0.13.9", "v0.13.8", 1, true},
		{"v0.13.8", "v0.14.0", -1, true},
		{"v1.0.0", "v0.99.99", 1, true},
		{"v0.13.10", "v0.13.9", 1, true},
		{"v1.0.0", "v1.0.0-rc.1", 1, true},
		{"v1.0.0-rc.1", "v1.0.0-rc.2", -1, true},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", 1, true},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1, true},
		{"v1.0.0-1", "v1.0.0-alpha", -1, true},
		{"v1.0.0+build.5", "v1.0.0", 0, true},
		{"dev", "v1.0.0", 0, false},
		{"v1.0", "v1.0.0", 0, false},
		{"v1.0.0-", "v1.0.0", 0, false},
		{"", "v1.0.0", 0, false},
	}
	for _, tc := range cases {
		got, ok := Compare(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Compare(%q, %q) = (%d, %v), want (%d, %v)", tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v0.13.9", "v0.13.8") {
		t.Fatal("v0.13.9 should be newer than v0.13.8")
	}
	if IsNewer("v0.13.8", "v0.13.9") {
		t.Fatal("a local build ahead of the latest tag must not report an update")
	}
	if IsNewer("v0.13.8", "unknown") {
		t.Fatal("an unparseable current version must not claim an update")
	}
}

func TestFetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.14.0","html_url":"https://github.com/cnjack/jcode/releases/tag/v0.14.0","published_at":"2026-09-01T00:00:00Z","body":"notes"}`))
	}))
	defer srv.Close()

	info, err := fetchLatest(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.Tag != "v0.14.0" || info.PublishedAt.IsZero() {
		t.Fatalf("unexpected release info: %+v", info)
	}
}

func TestTagURL(t *testing.T) {
	if got := TagURL(" v0.14.0 "); got != "https://github.com/cnjack/jcode/releases/tag/v0.14.0" {
		t.Fatalf("TagURL = %q", got)
	}
	if got := TagURL("../../evil"); strings.Contains(got, "/../") {
		t.Fatalf("TagURL must escape path segments, got %q", got)
	}
}

func TestFetchLatestErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"http status": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"missing tag": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"html_url":"x"}`)) },
		"bad json":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			if _, err := fetchLatest(context.Background(), srv.Client(), srv.URL); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
