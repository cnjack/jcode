package agent

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/adk/filesystem"
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
