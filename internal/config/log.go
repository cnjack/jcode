package config

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	appLogger *log.Logger
	logOnce   sync.Once
)

// Logger returns the shared application logger that writes to ~/.jcode/debug.log.
// It is initialised lazily and is safe for concurrent use.
func Logger() *log.Logger {
	logOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			appLogger = log.New(os.Stderr, "", log.LstdFlags)
			return
		}
		dir := filepath.Join(home, configDir)
		_ = os.MkdirAll(dir, 0o700)
		f, err := os.OpenFile(filepath.Join(dir, "debug.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			appLogger = log.New(os.Stderr, "", log.LstdFlags)
			return
		}
		appLogger = log.New(f, "", log.LstdFlags)
		// Redirect the default stdlib logger so third-party libraries that call
		// log.Printf directly (e.g. eino's retry_chatmodel) also write to the
		// debug log file instead of appearing on stderr/stdout.
		log.SetOutput(f)
		log.SetFlags(log.LstdFlags)
	})
	return appLogger
}
