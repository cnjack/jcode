//go:build !jcode_eval

package command

import (
	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
)

func computerEvalEnabled() bool { return false }

func installEvalComputerBackend(_ *computer.Manager, _ *config.Config) error { return nil }
