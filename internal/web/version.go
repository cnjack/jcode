package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/release"
)

// versionResponse is the GET /api/version payload. Build fields are always
// present; the latest-release fields are filled only for ?check=1, so opening
// Settings never contacts GitHub without an explicit user action.
type versionResponse struct {
	Version         string `json:"version"`
	GitCommit       string `json:"git_commit,omitempty"`
	BuildTime       string `json:"build_time,omitempty"`
	Checked         bool   `json:"checked"`
	Latest          string `json:"latest,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	CheckError      string `json:"check_error,omitempty"`
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	resp := versionResponse{
		Version:   s.version,
		GitCommit: stampedBuildValue(s.gitCommit),
		BuildTime: stampedBuildValue(s.buildTime),
	}
	if check := r.URL.Query().Get("check"); check == "1" || check == "true" {
		s.fillLatestRelease(r.Context(), &resp)
	}
	writeJSON(w, http.StatusOK, resp)
}

// fillLatestRelease records the outcome of a latest-release lookup. A failed
// lookup still answers 200 with check_error so the page keeps showing the
// running build's version.
func (s *Server) fillLatestRelease(ctx context.Context, resp *versionResponse) {
	lookup := s.latestRelease
	if lookup == nil {
		lookup = func(ctx context.Context) (release.Info, error) { return release.Latest(ctx, nil) }
	}
	ctx, cancel := context.WithTimeout(ctx, release.DefaultTimeout)
	defer cancel()

	resp.Checked = true
	info, err := lookup(ctx)
	if err != nil {
		config.Logger().Printf("[web] version check failed: %v", err)
		resp.CheckError = err.Error()
		return
	}
	resp.Latest = info.Tag
	resp.ReleaseURL = release.TagURL(info.Tag)
	if !info.PublishedAt.IsZero() {
		resp.PublishedAt = info.PublishedAt.UTC().Format(time.RFC3339)
	}
	resp.UpdateAvailable = release.IsNewer(info.Tag, s.version)
}

// stampedBuildValue hides the "unknown" placeholder that unstamped (go run /
// go build without ldflags) binaries carry.
func stampedBuildValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "unknown" {
		return ""
	}
	return v
}
