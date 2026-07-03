package browser

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cnjack/jcode/internal/config"
)

func (b *Bridge) tokenFile() string {
	if b.tokenPath != "" {
		return b.tokenPath
	}
	return filepath.Join(config.ConfigDir(), "browser", "ext-tokens.json")
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
