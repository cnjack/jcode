package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/release"
)

func readVersionPayload(t *testing.T, srv *Server, target string) versionResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	srv.handleVersion(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("version status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	var payload versionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	return payload
}

func TestVersionReportsBuildInfoWithoutContactingGitHub(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := &Server{
		version:   "v0.13.8",
		gitCommit: "abc1234",
		buildTime: "unknown",
		latestRelease: func(context.Context) (release.Info, error) {
			t.Fatal("a plain /api/version must not look up the latest release")
			return release.Info{}, nil
		},
	}

	got := readVersionPayload(t, srv, "/api/version")
	if got.Version != "v0.13.8" || got.GitCommit != "abc1234" {
		t.Fatalf("unexpected build info: %+v", got)
	}
	if got.BuildTime != "" {
		t.Fatalf("unstamped build time should be omitted, got %q", got.BuildTime)
	}
	if got.Checked || got.Latest != "" || got.UpdateAvailable {
		t.Fatalf("no check was requested: %+v", got)
	}
}

func TestVersionCheckReportsAvailableUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	published := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	srv := &Server{
		version: "v0.13.8",
		latestRelease: func(context.Context) (release.Info, error) {
			return release.Info{Tag: "v0.14.0", PublishedAt: published}, nil
		},
	}

	got := readVersionPayload(t, srv, "/api/version?check=1")
	if !got.Checked || !got.UpdateAvailable || got.Latest != "v0.14.0" {
		t.Fatalf("expected an available update: %+v", got)
	}
	if got.ReleaseURL != "https://github.com/cnjack/jcode/releases/tag/v0.14.0" {
		t.Fatalf("release_url = %q", got.ReleaseURL)
	}
	if got.PublishedAt != "2026-09-20T08:00:00Z" {
		t.Fatalf("published_at = %q", got.PublishedAt)
	}
}

func TestVersionCheckUpToDateAndAheadOfLatest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, current := range []string{"v0.14.0", "0.14.0", "v0.14.1"} {
		srv := &Server{
			version: current,
			latestRelease: func(context.Context) (release.Info, error) {
				return release.Info{Tag: "v0.14.0"}, nil
			},
		}
		got := readVersionPayload(t, srv, "/api/version?check=true")
		if !got.Checked || got.UpdateAvailable || got.CheckError != "" {
			t.Fatalf("current %s: expected up to date, got %+v", current, got)
		}
	}
}

func TestVersionCheckFailureKeepsBuildInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := &Server{
		version: "v0.13.8",
		latestRelease: func(context.Context) (release.Info, error) {
			return release.Info{}, errors.New("release: GitHub API returned HTTP 403")
		},
	}

	got := readVersionPayload(t, srv, "/api/version?check=1")
	if got.Version != "v0.13.8" || !got.Checked || got.CheckError == "" || got.UpdateAvailable {
		t.Fatalf("expected a reported check failure with build info intact: %+v", got)
	}
}
