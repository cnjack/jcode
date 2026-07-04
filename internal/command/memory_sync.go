package command

import (
	"context"
	"fmt"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory/pipeline"
)

// runMemorySync drives the offline distillation pipeline (design §5).
func runMemorySync(ctx context.Context, cfg *config.Config, projectDir string, wait, includeRecent bool) error {
	opts := pipeline.Options{
		IncludeRecent:  includeRecent,
		IgnoreCooldown: true, // manual sync is an explicit user request
		Log: func(f string, a ...any) {
			fmt.Printf(f+"\n", a...)
		},
	}
	// A CLI process cannot outlive itself: sync always runs in the foreground.
	// --wait is accepted for script compatibility.
	_ = wait
	return pipeline.Run(ctx, cfg, projectDir, opts)
}
