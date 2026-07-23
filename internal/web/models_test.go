package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
)

func TestWebSwitchModelSameValueIsNoOp(t *testing.T) {
	s := &Server{
		Engine:   &Engine{providerName: "openai", modelName: "gpt-5"},
		wsBroker: NewWSBroker(),
	}
	rec := httptest.NewRecorder()
	s.handleSwitchModel(rec, httptest.NewRequest(http.MethodPost, "/api/model",
		strings.NewReader(`{"provider":"openai","model":"gpt-5"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("same model: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestProviderCatalogUsesPersistedModelVisibility(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, "config.json"),
		[]byte(`{"providers":{"zhipuai-coding-plan":{"api_key":"test"}}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	state := &config.ModelState{
		EnabledModels: []config.ModelRef{{
			Provider: "zhipuai-coding-plan",
			Model:    "glm-4.5-air",
		}},
		DisabledModels: []config.ModelRef{{
			Provider: "zhipuai-coding-plan",
			Model:    "glm-5.2",
		}},
	}
	if err := config.SaveModelState(state); err != nil {
		t.Fatal(err)
	}

	registry := model.NewModelRegistry()
	s := &Server{registry: registry}
	req := httptest.NewRequest(http.MethodGet, "/api/providers/zhipuai-coding-plan/models", nil)
	req.SetPathValue("id", "zhipuai-coding-plan")
	rec := httptest.NewRecorder()
	s.handleProviderCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: code=%d body=%q", rec.Code, rec.Body.String())
	}

	var got []struct {
		ID    string `json:"id"`
		Added bool   `json:"added"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	addedByID := make(map[string]bool, len(got))
	for _, item := range got {
		addedByID[item.ID] = item.Added
	}
	if !addedByID["glm-4.5-air"] {
		t.Error("explicitly enabled glm-4.5-air was reported disabled")
	}
	if addedByID["glm-5.2"] {
		t.Error("explicitly disabled default glm-5.2 was reported enabled")
	}
}
