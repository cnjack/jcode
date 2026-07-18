package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/browser"
	"github.com/cnjack/jcode/internal/config"
)

func TestBrowserConfigRefreshesLiveAgentAndRuntimeDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rebuilds := 0
	eng := &Engine{
		taskID: "active",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			rebuilds++
			return nil, nil
		},
	}
	mgr := browser.NewManager(browser.Config{})
	t.Cleanup(func() { _ = mgr.Close() })
	s := &Server{Engine: eng, cfg: &config.Config{}, browserMgr: mgr}

	rec := httptest.NewRecorder()
	s.handleBrowserConfig(rec, httptest.NewRequest(http.MethodPost, "/api/browser/config",
		strings.NewReader(`{"enabled":false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rebuilds != 1 {
		t.Fatalf("agent rebuilds=%d, want 1", rebuilds)
	}
	got := mgr.GetConfig()
	if got.Enabled || got.Backend != "auto" || got.Viewport != "1280x720" {
		t.Fatalf("runtime browser config=%+v", got)
	}
}

func TestBrowserConfigReturnsStableAgentRefreshWarningCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	eng := &Engine{
		taskID: "active",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			return nil, errors.New("provider unavailable")
		},
	}
	mgr := browser.NewManager(browser.Config{})
	t.Cleanup(func() { _ = mgr.Close() })
	s := &Server{Engine: eng, cfg: &config.Config{}, browserMgr: mgr}

	rec := httptest.NewRecorder()
	s.handleBrowserConfig(rec, httptest.NewRequest(http.MethodPost, "/api/browser/config",
		strings.NewReader(`{"enabled":false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		WarningCode string `json:"warning_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.WarningCode != "agent_refresh_failed" {
		t.Fatalf("warning_code=%q, want agent_refresh_failed", response.WarningCode)
	}
}
