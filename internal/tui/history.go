package tui

import (
	"os"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

func loadHistory() []string {
	hPath, err := config.HistoryFilePath()
	if err != nil {
		return nil
	}
	content, err := os.ReadFile(hPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	var history []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			history = append(history, l)
		}
	}
	return history
}

func appendHistory(prompt string) {
	hPath, err := config.HistoryFilePath()
	if err != nil {
		return
	}
	f, err := os.OpenFile(hPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(prompt + "\n")
}
