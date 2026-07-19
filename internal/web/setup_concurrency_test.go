package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
)

const setupCustomProviderBody = `{
  "provider":"setup-custom",
  "model":"setup-model",
  "api_key":"test-key",
  "base_url":"https://example.invalid/v1"
}`

func newSetupTestServer() *Server {
	return &Server{
		Engine: &Engine{
			taskID: "setup-task",
			createAgent: func(string, string) (*adk.ChatModelAgent, error) {
				return new(adk.ChatModelAgent), nil
			},
		},
		cfg:        &config.Config{MaxIterations: 1000},
		needsSetup: true,
		wsBroker:   NewWSBroker(),
	}
}

func saveToolSearchForSetupTest(t *testing.T, s *Server) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleToolSearchConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tool-search/config",
		strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("ToolSearch save status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func completeSetupForTest(s *Server) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleSetupComplete(rec, httptest.NewRequest(http.MethodPost, "/api/setup/complete",
		strings.NewReader(setupCustomProviderBody)))
	return rec
}

func assertSetupAndToolSearchPersisted(t *testing.T, s *Server) {
	t.Helper()
	s.cfgMu.Lock()
	live := s.cfg
	hasProvider := live != nil && live.GetProviders()["setup-custom"] != nil
	hasToolSearch := live != nil && config.ToolSearchEnabled(live)
	s.cfgMu.Unlock()
	if !hasProvider || !hasToolSearch {
		t.Fatalf("live config lost fields: provider=%v tool_search=%v", hasProvider, hasToolSearch)
	}
	disk, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if disk.GetProviders()["setup-custom"] == nil || !config.ToolSearchEnabled(disk) {
		t.Fatalf("disk config lost setup/settings fields: %+v", disk)
	}
}

func TestSetupPreservesSettingsSavedBeforeFirstProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSetupTestServer()
	saveToolSearchForSetupTest(t, s)
	rec := completeSetupForTest(s)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertSetupAndToolSearchPersisted(t, s)
}

func TestSetupAndSettingsConcurrentSavesPreserveBoth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSetupTestServer()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var setupRec, settingRec *httptest.ResponseRecorder
	go func() {
		defer wg.Done()
		<-start
		setupRec = completeSetupForTest(s)
	}()
	go func() {
		defer wg.Done()
		<-start
		settingRec = httptest.NewRecorder()
		s.handleToolSearchConfig(settingRec, httptest.NewRequest(http.MethodPost, "/api/tool-search/config",
			strings.NewReader(`{"enabled":true}`)))
	}()
	close(start)
	wg.Wait()
	if setupRec.Code != http.StatusOK || settingRec.Code != http.StatusOK {
		t.Fatalf("setup=%d %s settings=%d %s", setupRec.Code, setupRec.Body.String(), settingRec.Code, settingRec.Body.String())
	}
	assertSetupAndToolSearchPersisted(t, s)
}
