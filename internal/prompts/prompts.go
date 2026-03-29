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
	utils "github.com/cnjack/coding/internal/util"
)

//go:embed system.md
var systemPrompt string

//go:embed plan.md
var planPrompt string

func GetSystemPrompt(platform, pwd, envLabel string, envInfo *utils.EnvInfo) string {
	t, err := template.New("template").Parse(systemPrompt)
	if err != nil {
		return ""
	}

	cfg, _ := config.LoadConfig()
	var sshAliases []config.SSHAlias
	if cfg != nil {
		sshAliases = cfg.SSHAliases
	}

	data := struct {
		Platform    string
		Pwd         string
		Date        string
		EnvLabel    string
		SSHAliases  []config.SSHAlias
		GitBranch   string
		GitDirty    bool
		LastCommit  string
		ProjectType string
		DirTree     string
	}{
		Platform:   platform,
		Pwd:        pwd,
		Date:       time.Now().Format("2006-01-02"),
		EnvLabel:   envLabel,
		SSHAliases: sshAliases,
	}

	if envInfo != nil {
		data.GitBranch = envInfo.GitBranch
		data.GitDirty = envInfo.GitDirty
		data.LastCommit = envInfo.LastCommit
		data.ProjectType = envInfo.ProjectType
		data.DirTree = envInfo.DirTree
	}

	var stringBuffer = bytes.NewBuffer(nil)
	err = t.Execute(stringBuffer, data)
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

// GetPlanSystemPrompt returns the system prompt for Plan mode (read-only exploration).
func GetPlanSystemPrompt(platform, pwd, envLabel string, envInfo *utils.EnvInfo) string {
	t, err := template.New("plan").Parse(planPrompt)
	if err != nil {
		return ""
	}

	data := struct {
		Platform    string
		Pwd         string
		Date        string
		EnvLabel    string
		GitBranch   string
		GitDirty    bool
		LastCommit  string
		ProjectType string
		DirTree     string
	}{
		Platform: platform,
		Pwd:      pwd,
		Date:     time.Now().Format("2006-01-02"),
		EnvLabel: envLabel,
	}

	if envInfo != nil {
		data.GitBranch = envInfo.GitBranch
		data.GitDirty = envInfo.GitDirty
		data.LastCommit = envInfo.LastCommit
		data.ProjectType = envInfo.ProjectType
		data.DirTree = envInfo.DirTree
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return ""
	}
	result := buf.String()

	if content := loadAgentsMd(pwd); content != "" {
		result += "\n\n## Custom Agent Instructions\n\n" + content
	}
	return result
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
