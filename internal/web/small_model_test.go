package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

func newSmallModelTestServer() *Server {
	return &Server{
		cfg: &config.Config{
			Model: "zai/glm-5.2",
			Providers: map[string]*config.ProviderConfig{
				"zai": {APIKey: "k"},
			},
		},
	}
}

func postSmallModel(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSetSmallModel(rec, httptest.NewRequest(http.MethodPost, "/api/small-model", strings.NewReader(body)))
	return rec
}

func TestSetSmallModelPersistsAndMutatesLiveConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSmallModelTestServer()
	live := s.cfg // the pointer task builders capture; must be updated in place

	rec := postSmallModel(t, s, `{"provider":"zai","model":"glm-4.6v"}`)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if live.SmallModel != "zai/glm-4.6v" {
		t.Errorf("live config not mutated in place: SmallModel=%q", live.SmallModel)
	}
	onDisk, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if onDisk.SmallModel != "zai/glm-4.6v" {
		t.Errorf("not persisted: disk SmallModel=%q", onDisk.SmallModel)
	}

	// GET /api/config surfaces the saved value.
	get := httptest.NewRecorder()
	s.handleGetConfig(get, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfgResp struct {
		SmallModel string `json:"small_model"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &cfgResp); err != nil {
		t.Fatalf("config response: %v", err)
	}
	if cfgResp.SmallModel != "zai/glm-4.6v" {
		t.Errorf("GET /api/config small_model=%q", cfgResp.SmallModel)
	}

	// Clear with both fields empty.
	rec = postSmallModel(t, s, `{"provider":"","model":""}`)
	if rec.Code != 200 {
		t.Fatalf("clear: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if live.SmallModel != "" {
		t.Errorf("clear: live SmallModel=%q", live.SmallModel)
	}
	onDisk, _ = config.LoadConfig()
	if onDisk.SmallModel != "" {
		t.Errorf("clear not persisted: disk SmallModel=%q", onDisk.SmallModel)
	}
}

func TestSetSmallModelValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cases := []struct {
		name string
		body string
	}{
		{"unknown provider", `{"provider":"nope","model":"m"}`},
		{"provider without model", `{"provider":"zai","model":""}`},
		{"model without provider", `{"provider":"","model":"glm-4.6v"}`},
		{"garbage body", `{"provider":`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newSmallModelTestServer()
			s.cfg.SmallModel = "zai/glm-4.6v"
			rec := postSmallModel(t, s, c.body)
			if rec.Code != 400 {
				t.Fatalf("code=%d body=%q, want 400", rec.Code, rec.Body.String())
			}
			if s.cfg.SmallModel != "zai/glm-4.6v" {
				t.Errorf("rejected request mutated config: SmallModel=%q", s.cfg.SmallModel)
			}
		})
	}
}

func TestSetSmallModelNilConfig(t *testing.T) {
	s := &Server{}
	rec := postSmallModel(t, s, `{"provider":"zai","model":"glm-4.6v"}`)
	if rec.Code != 500 {
		t.Fatalf("code=%d body=%q, want 500", rec.Code, rec.Body.String())
	}
}
