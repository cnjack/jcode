package agent

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/schema"

	internalmodel "github.com/cnjack/jcode/internal/model"
)

// LocalReductionBackend implements reduction.Backend by writing truncated
// tool output to local files so the agent can re-read them via the read tool.
type LocalReductionBackend struct {
	RootDir string
}

func (b *LocalReductionBackend) Write(_ context.Context, req *filesystem.WriteRequest) error {
	fp := req.FilePath
	if !filepath.IsAbs(fp) {
		fp = filepath.Join(b.RootDir, fp)
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o700); err != nil {
		return err
	}
	return os.WriteFile(fp, []byte(req.Content), 0o600)
}

// DefaultMaxLengthForTrunc is the per-result character cap: tool outputs above
// this are offloaded to a file and truncated in the conversation.
const DefaultMaxLengthForTrunc = 50000

// ReductionExcludeTools lists tools whose results must never be truncated:
// their content is irreplaceable direct input (a user's answer, a loaded skill
// body), not re-derivable command output. Shared by the reduction middleware's
// TruncExcludeTools and the per-turn budget middleware.
var ReductionExcludeTools = []string{"ask_user", "load_skill"}

// ReductionThreshold derives the reduction (tool-output clearing) trigger
// fraction from the compaction threshold: the lighter, earlier clearing sits
// 0.15 below compaction; when that would drop under 0.1 it falls back to 80%
// of the compaction threshold. Single source for what was previously
// copy-pasted (and drifting) across the TUI/ACP/web surfaces.
func ReductionThreshold(compactThreshold float64) float64 {
	rt := compactThreshold - 0.15
	if rt < 0.1 {
		rt = compactThreshold * 0.8
	}
	return rt
}

// BuildReductionConfig is the single source for the reduction middleware
// configuration shared by all surfaces (TUI/ACP/web). rootDir is where
// offloaded content is written (both the Backend fallback root and the
// offload path base). contextLimit is the RAW model window; the clear trigger
// is computed on the EFFECTIVE window (output headroom reserved) so it uses
// the same base as the summarization trigger. counter is the TokenCounter to
// inject; nil keeps eino's default (len/4).
//
// It returns the config rather than the constructed middleware so callers can
// wrap it in reduction.New and tests can assert fields directly.
func BuildReductionConfig(
	rootDir string,
	contextLimit int,
	compactThreshold float64,
	counter func(ctx context.Context, msgs []*schema.Message, tools []*schema.ToolInfo) (int64, error),
) *reduction.Config {
	effLimit := internalmodel.EffectiveContextLimit(contextLimit)
	return &reduction.Config{
		Backend:           &LocalReductionBackend{RootDir: rootDir},
		RootDir:           rootDir,
		MaxLengthForTrunc: DefaultMaxLengthForTrunc,
		MaxTokensForClear: int64(float64(effLimit) * ReductionThreshold(compactThreshold)),
		ReadFileToolName:  "read",
		TruncExcludeTools: append([]string(nil), ReductionExcludeTools...),
		TokenCounter:      counter,
		ToolConfig: map[string]*reduction.ToolReductionConfig{
			"read": {SkipClear: true},
		},
	}
}
