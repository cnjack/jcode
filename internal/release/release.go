// Package release looks up published jcode releases on GitHub and compares
// build versions. It is shared by `jcode update` and the web settings page.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LatestURL is the GitHub API endpoint for the newest non-prerelease release.
const LatestURL = "https://api.github.com/repos/cnjack/jcode/releases/latest"

// ReleasesURL is the public release listing; TagURL links to one release.
const ReleasesURL = "https://github.com/cnjack/jcode/releases"

// DefaultTimeout bounds a latest-release lookup when the caller's context has
// no deadline of its own.
const DefaultTimeout = 10 * time.Second

// Info describes a published release.
type Info struct {
	Tag         string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
}

// TagURL returns the release page for tag. It is built from the fixed
// repository URL so callers never open an API-supplied link.
func TagURL(tag string) string {
	return ReleasesURL + "/tag/" + url.PathEscape(strings.TrimSpace(tag))
}

// Latest fetches the newest published release. A nil client uses
// http.DefaultClient.
func Latest(ctx context.Context, client *http.Client) (Info, error) {
	return fetchLatest(ctx, client, LatestURL)
}

func fetchLatest(ctx context.Context, client *http.Client, endpoint string) (Info, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Info{}, fmt.Errorf("release: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Info{}, fmt.Errorf("release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("release: GitHub API returned HTTP %d", resp.StatusCode)
	}
	var info Info
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return Info{}, fmt.Errorf("release: decode latest release: %w", err)
	}
	if strings.TrimSpace(info.Tag) == "" {
		return Info{}, fmt.Errorf("release: latest release has no tag")
	}
	return info, nil
}

// Normalize strips surrounding whitespace and a leading "v".
func Normalize(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// Compare orders two semantic versions ("v1.2.3", "1.2.3-rc.1", ...). It
// returns -1, 0, or 1 and ok=false when either side is not a valid version
// (e.g. an unstamped dev build), in which case callers should not claim an
// update relationship.
func Compare(a, b string) (cmp int, ok bool) {
	va, okA := parse(a)
	vb, okB := parse(b)
	if !okA || !okB {
		return 0, false
	}
	for i := range va.core {
		if va.core[i] != vb.core[i] {
			if va.core[i] < vb.core[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return comparePre(va.pre, vb.pre), true
}

// IsNewer reports whether latest is a strictly newer version than current.
func IsNewer(latest, current string) bool {
	cmp, ok := Compare(latest, current)
	return ok && cmp > 0
}

type version struct {
	core [3]int
	pre  []string
}

func parse(raw string) (version, bool) {
	s := Normalize(raw)
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata never affects precedence
	}
	var v version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
		for _, id := range v.pre {
			if id == "" {
				return version{}, false
			}
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return version{}, false
		}
		v.core[i] = n
	}
	return v, true
}

// comparePre applies SemVer §11 pre-release precedence: a release outranks
// any pre-release; identifiers compare numerically when both are numeric,
// numeric identifiers rank below alphanumeric ones, and a longer list wins a
// shared prefix.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		na, errA := strconv.Atoi(a[i])
		nb, errB := strconv.Atoi(b[i])
		switch {
		case errA == nil && errB == nil:
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
		case errA == nil:
			return -1
		case errB == nil:
			return 1
		default:
			if c := strings.Compare(a[i], b[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
