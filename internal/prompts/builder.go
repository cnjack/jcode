package prompts

import (
	"bytes"
	"context"
	"strings"
	"text/template"
	"time"

	"github.com/cnjack/jcode/internal/config"
	utils "github.com/cnjack/jcode/internal/util"
)

// PromptBlock represents a segment of the system prompt.
type PromptBlock struct {
	Content    string
	CacheScope string // "global", "session", or "" (no cache)
}

// PromptResult is the output of a prompt build.
type PromptResult struct {
	Blocks []PromptBlock
	Full   string // concatenated for backward compatibility
}

// BuilderConfig is the input for prompt construction.
type BuilderConfig struct {
	Platform          string
	Pwd               string
	EnvLabel          string
	SkillDescriptions string
	PlanMode          bool
	CacheEnabled      bool
}

// PromptBuilder constructs system prompts with parallel loading and caching.
type PromptBuilder struct {
	cfg       BuilderConfig
	envLoader *AsyncEnvLoader
	memLoader *MemoryLoader
	cache     *PromptBlockCache
}

// NewPromptBuilder creates a PromptBuilder with the given config.
// It reads prompt-related settings from the user's config file.
func NewPromptBuilder(cfg BuilderConfig) *PromptBuilder {
	userCfg, _ := config.LoadConfig()

	memCfg := MemoryConfig{
		MaxTotalChars: 40000,
		MaxIncDepth:   5,
	}
	envTimeout := 5 * time.Second

	if userCfg != nil && userCfg.Prompt != nil {
		if userCfg.Prompt.MemoryMaxChars > 0 {
			memCfg.MaxTotalChars = userCfg.Prompt.MemoryMaxChars
		}
		if userCfg.Prompt.MemoryMaxDepth > 0 {
			memCfg.MaxIncDepth = userCfg.Prompt.MemoryMaxDepth
		}
		if userCfg.Prompt.AsyncEnvTimeout != "" {
			if d, err := time.ParseDuration(userCfg.Prompt.AsyncEnvTimeout); err == nil {
				envTimeout = d
			}
		}
		cfg.CacheEnabled = userCfg.Prompt.CacheEnabled
	}

	return &PromptBuilder{
		cfg:       cfg,
		envLoader: NewAsyncEnvLoader(envTimeout),
		memLoader: NewMemoryLoader(memCfg),
		cache:     NewPromptBlockCache(),
	}
}

// Build constructs the system prompt.
// It loads env info and AGENTS.md in parallel, renders the template,
// and optionally caches static blocks.
func (b *PromptBuilder) Build(ctx context.Context) (*PromptResult, error) {
	type envResult struct {
		info *utils.EnvInfo
	}
	type memResult struct {
		content string
		err     error
	}

	envCh := make(chan envResult, 1)
	memCh := make(chan memResult, 1)

	// Load env info in parallel.
	go func() {
		info := b.envLoader.Load(ctx, b.cfg.Pwd)
		envCh <- envResult{info: info}
	}()

	// Load AGENTS.md in parallel.
	go func() {
		content, err := b.memLoader.Load(b.cfg.Pwd)
		memCh <- memResult{content: content, err: err}
	}()

	envRes := <-envCh
	memRes := <-memCh

	if memRes.err != nil {
		config.Logger().Printf("[builder] memory load error: %v", memRes.err)
	}

	// Select template.
	tmplSrc := systemPrompt
	if b.cfg.PlanMode {
		tmplSrc = planPrompt
	}

	// Render the core template.
	coreContent, err := b.renderTemplate(tmplSrc, envRes.info)
	if err != nil {
		return nil, err
	}

	var blocks []PromptBlock

	// Core template block — may be cached if enabled.
	if b.cfg.CacheEnabled {
		hash := ContentHash(coreContent)
		block := b.cache.GetOrBuild(hash, func() *PromptBlock {
			return &PromptBlock{Content: coreContent, CacheScope: "global"}
		})
		blocks = append(blocks, *block)
	} else {
		blocks = append(blocks, PromptBlock{Content: coreContent})
	}

	// AGENTS.md block (session-scoped, changes per project).
	if memRes.content != "" {
		agentsBlock := PromptBlock{
			Content:    "\n\n## Custom Agent Instructions\n\n" + memRes.content,
			CacheScope: "session",
		}
		blocks = append(blocks, agentsBlock)
	}

	// Build full string for backward compatibility.
	var full strings.Builder
	for _, blk := range blocks {
		full.WriteString(blk.Content)
	}

	return &PromptResult{
		Blocks: blocks,
		Full:   full.String(),
	}, nil
}

// renderTemplate renders the chosen prompt template with current data.
func (b *PromptBuilder) renderTemplate(tmplSrc string, envInfo *utils.EnvInfo) (string, error) {
	t, err := template.New("prompt").Parse(tmplSrc)
	if err != nil {
		return "", err
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
		Platform:          b.cfg.Platform,
		Pwd:               b.cfg.Pwd,
		Date:              time.Now().Format("2006-01-02"),
		EnvLabel:          b.cfg.EnvLabel,
		SSHAliases:        sshAliases,
		SkillDescriptions: b.cfg.SkillDescriptions,
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
		return "", err
	}
	return buf.String(), nil
}
