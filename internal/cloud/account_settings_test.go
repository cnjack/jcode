package cloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

type accountSettingsFixture struct {
	mu       sync.Mutex
	version  int64
	envelope json.RawMessage
}

func (f *accountSettingsFixture) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"version": f.version, "envelope": f.envelope})
	case http.MethodPut:
		var req struct {
			BaseVersion int64           `json:"base_version"`
			Envelope    json.RawMessage `json:"envelope"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.BaseVersion != f.version {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "settings_conflict"}})
			return
		}
		f.version++
		f.envelope = append(json.RawMessage(nil), req.Envelope...)
		_ = json.NewEncoder(w).Encode(map[string]any{"version": f.version, "envelope": f.envelope})
	}
}

func TestAccountSettingsSyncIsEncryptedAndAppliesPortableWhitelist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	key := []byte("0123456789abcdef0123456789abcdef")
	cipher, err := NewEnvelopeCipher(key, 1)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &accountSettingsFixture{}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()

	first := &config.Config{
		Providers: map[string]*config.ProviderConfig{"openai": {}},
		Model:     "openai/gpt-5", SmallModel: "openai/gpt-5-mini",
		Language: "zh-Hans", Theme: "jcode-dark", DefaultMode: "approval",
		AccountSettingsUpdatedAt: time.Now().UTC(),
	}
	connector := NewConnector(ConnectorConfig{
		CloudURL: server.URL, Credentials: &Credentials{DeviceToken: "jcd_test"},
		AppConfig: first, Cipher: cipher,
	})
	if err := connector.ReconcileAccountSettings(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	stored := string(fixture.envelope)
	fixture.mu.Unlock()
	for _, plaintext := range []string{"gpt-5", "zh-Hans", "jcode-dark"} {
		if strings.Contains(stored, plaintext) {
			t.Fatalf("cloud envelope leaks %q: %s", plaintext, stored)
		}
	}

	second := &config.Config{
		Providers: map[string]*config.ProviderConfig{"openai": {}},
		Model:     "openai/local", Language: "en", DefaultMode: "approval",
	}
	puller := NewConnector(ConnectorConfig{
		CloudURL: server.URL, Credentials: &Credentials{DeviceToken: "jcd_other"},
		AppConfig: second, Cipher: cipher,
	})
	if err := puller.ReconcileAccountSettings(t.Context()); err != nil {
		t.Fatal(err)
	}
	if second.Model != first.Model || second.SmallModel != first.SmallModel || second.Language != "zh-Hans" || second.Theme != "jcode-dark" {
		t.Fatalf("pulled whitelist = model %q small %q language %q theme %q", second.Model, second.SmallModel, second.Language, second.Theme)
	}
}

func TestAccountSettingsDoesNotApplyUnavailableProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]*config.ProviderConfig{"local": {}}, Model: "local/working",
	}
	prefs := AccountPreferences{
		SchemaVersion: 1, Model: "missing/gpt", SmallModel: "missing/mini",
		Language: "ja", Theme: "jcode-light", DefaultMode: "plan", UpdatedAt: time.Now().UTC(),
	}
	t.Setenv("HOME", t.TempDir())
	if err := applyAccountPreferences(cfg, prefs, 3); err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "local/working" || cfg.SmallModel != "" {
		t.Fatalf("unavailable provider overwrote models: model=%q small=%q", cfg.Model, cfg.SmallModel)
	}
	if cfg.Language != "ja" || cfg.DefaultMode != "plan" {
		t.Fatalf("portable fields not applied: %+v", cfg)
	}
}
