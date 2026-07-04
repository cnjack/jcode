package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

func (b *Bridge) tokenFile() string {
	if b.tokenPath != "" {
		return b.tokenPath
	}
	return filepath.Join(config.ConfigDir(), "browser", "ext-tokens.json")
}

func (b *Bridge) stableTokenFile() string {
	return filepath.Join(config.ConfigDir(), "browser", "server-token")
}

// StableToken returns one long-lived token, reused across restarts — the "key"
// the extension stores once and re-presents forever. Combined with a stable
// server port, the extension reconnects silently with no re-auth. Persisted to
// ~/.jcode/browser/server-token (0600) and kept in the valid-token set.
// Preferred over IssueToken for the native/auto-connect path so tokens don't
// accumulate a fresh entry on every launch.
func (b *Bridge) StableToken() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if data, err := os.ReadFile(b.stableTokenFile()); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			b.tokens[tok] = true
			return tok
		}
	}
	tok := randomToken()
	b.tokens[tok] = true
	b.saveTokensLocked()
	path := b.stableTokenFile()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(tok), 0o600)
	return tok
}

func (b *Bridge) loadTokens() {
	data, err := os.ReadFile(b.tokenFile())
	if err != nil {
		return
	}
	var toks []string
	if json.Unmarshal(data, &toks) != nil {
		return
	}
	for _, t := range toks {
		b.tokens[t] = true
	}
}

// saveTokensLocked persists tokens; caller holds b.mu.
func (b *Bridge) saveTokensLocked() {
	toks := make([]string, 0, len(b.tokens))
	for t := range b.tokens {
		toks = append(toks, t)
	}
	data, err := json.Marshal(toks)
	if err != nil {
		return
	}
	path := b.tokenFile()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o600)
}
