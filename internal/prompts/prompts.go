package prompts

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
	utils "github.com/cnjack/jcode/internal/util"
)

//go:embed system.md
var systemPrompt string

//go:embed plan.md
var planPrompt string

func GetSystemPrompt(platform, pwd, envLabel string, envInfo *utils.EnvInfo, skillDescriptions string) string {
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
		Platform          string
		Pwd               string
		Date              string
		EnvLabel          string
		SSHAliases        []config.SSHAlias
		GitBranch         string
		GitDirty          bool
		LastCommit        string
		ProjectType       string
		DirTree           string
		SkillDescriptions string
	}{
		Platform:          platform,
		Pwd:               pwd,
		Date:              time.Now().Format("2006-01-02"),
		EnvLabel:          envLabel,
		SSHAliases:        sshAliases,
		SkillDescriptions: skillDescriptions,
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
	// Inject learned cross-session memory (transient: system prompt only,
	// never part of the session history). AGENTS.md stays authoritative —
	// the memory section explicitly yields to it.
	result += memory.BuildInjection(pwd, cfg)
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
	// Plan mode is read-only (no memory_note tool) but still benefits from
	// knowing what prior sessions learned about this project.
	planCfg, _ := config.LoadConfig()
	result += memory.BuildInjection(pwd, planCfg)
	return result
}

// LoadAgentsMdContent loads the merged agent-instruction content for pwd via
// the same MemoryLoader pipeline (char limits, @include resolution) that feeds
// the system prompt. Exported for the reminder middleware's AGENTS.md reload
// check, so both paths always see identical content.
func LoadAgentsMdContent(pwd string) string {
	return loadAgentsMd(pwd)
}

func loadAgentsMd(pwd string) string {
	loader := NewMemoryLoader(MemoryConfig{
		MaxTotalChars: 40000,
		MaxIncDepth:   5,
	})
	content, err := loader.Load(pwd)
	if err != nil {
		return ""
	}
	return content
}

// SerializeEnvInfo produces a stable string representation of environment info
// for storage in session entries. The format is simple key=value lines.
func SerializeEnvInfo(platform, pwd, envLabel string, envInfo *utils.EnvInfo) string {
	var sb strings.Builder
	sb.WriteString("platform=" + platform + "\n")
	sb.WriteString("pwd=" + pwd + "\n")
	sb.WriteString("date=" + time.Now().Format("2006-01-02") + "\n")
	sb.WriteString("env_label=" + envLabel + "\n")
	if envInfo != nil {
		sb.WriteString("git_branch=" + envInfo.GitBranch + "\n")
		if envInfo.GitDirty {
			sb.WriteString("git_dirty=true\n")
		} else {
			sb.WriteString("git_dirty=false\n")
		}
		sb.WriteString("last_commit=" + envInfo.LastCommit + "\n")
		sb.WriteString("project_type=" + envInfo.ProjectType + "\n")
		// DirTree omitted from diff — too noisy and changes often.
	}
	return sb.String()
}

// parseEnvKV parses a key=value env info string into a map.
func parseEnvKV(s string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			m[line[:idx]] = line[idx+1:]
		}
	}
	return m
}

// BuildEnvDiff compares a stored environment snapshot (from session) with
// the current environment and returns a human-readable diff string.
// Returns "" if nothing changed.
func BuildEnvDiff(storedEnvInfo string, platform, pwd, envLabel string, envInfo *utils.EnvInfo) string {
	currentEnvInfo := SerializeEnvInfo(platform, pwd, envLabel, envInfo)
	if storedEnvInfo == currentEnvInfo {
		return ""
	}

	stored := parseEnvKV(storedEnvInfo)
	current := parseEnvKV(currentEnvInfo)

	var diffs []string
	keys := []string{"date", "git_branch", "git_dirty", "last_commit", "project_type", "pwd", "env_label"}
	for _, k := range keys {
		sv, cv := stored[k], current[k]
		if sv != cv {
			diffs = append(diffs, k+": "+sv+" → "+cv)
		}
	}

	if len(diffs) == 0 {
		return ""
	}
	return "Environment changes since your context was last updated:\n" + strings.Join(diffs, "\n")
}
