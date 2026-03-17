package prompts

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/cnjack/coding/internal/config"
)

//go:embed system.md
var systemPrompt string

func GetSystemPrompt(platform, pwd, envLabel string) string {
	t, err := template.New("template").Parse(systemPrompt)
	if err != nil {
		return ""
	}
	
	cfg, _ := config.LoadConfig()
	var sshAliases []config.SSHAlias
	if cfg != nil {
		sshAliases = cfg.SSHAliases
	}

	var stringBuffer = bytes.NewBuffer(nil)
	err = t.Execute(stringBuffer, struct {
		Platform   string
		Pwd        string
		Date       string
		EnvLabel   string
		SSHAliases []config.SSHAlias
	}{
		Platform:   platform,
		Pwd:        pwd,
		Date:       time.Now().Format("2006-01-02"),
		EnvLabel:   envLabel,
		SSHAliases: sshAliases,
	})
	if err != nil {
		return ""
	}
	result := stringBuffer.String()

	// Inject agents.md if present in the working directory (case-insensitive).
	if content := loadAgentsMd(pwd); content != "" {
		result += "\n\n## Custom Agent Instructions\n\n" + content
	}
	return result
}

// HasAgentsMd returns the path to agents.md (case-insensitive) in pwd, or "".
func HasAgentsMd(pwd string) string {
	entries, err := os.ReadDir(pwd)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "agents.md") {
			return filepath.Join(pwd, e.Name())
		}
	}
	return ""
}

func loadAgentsMd(pwd string) string {
	p := HasAgentsMd(pwd)
	if p == "" {
		return ""
	}
	content, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(content)
}
