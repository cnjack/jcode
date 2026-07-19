package config

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

var (
	appLogger *log.Logger
	logOnce   sync.Once

	// loggingEnabled gates whether Logger() writes to ~/.jcode/debug.log. It
	// defaults to true (the historical always-on behavior) and is published
	// once per process from the loaded config at startup via SetLoggingEnabled.
	// Runtime toggling of this flag has no effect on an already-initialised
	// logger; the Developer settings UI marks the toggle as "restart required".
	loggingEnabled atomic.Bool
)

func init() {
	loggingEnabled.Store(true)
}

// SetLoggingEnabled publishes the logging preference. Call once at startup
// before the first Logger() call; subsequent calls do not re-initialise the
// logger. See internal/command/{web,interactive,acp}.go.
func SetLoggingEnabled(enabled bool) {
	loggingEnabled.Store(enabled)
}

// Logger returns the shared application logger that writes to ~/.jcode/debug.log
// (or an io.Discard sink when the user has disabled logging via Settings →
// Developer). It is initialised lazily and is safe for concurrent use.
func Logger() *log.Logger {
	logOnce.Do(func() {
		if !loggingEnabled.Load() {
			appLogger = log.New(io.Discard, "", log.LstdFlags)
			// Keep third-party libraries quiet too — they would otherwise leak
			// to stderr/stdout, which the TUI owns.
			log.SetOutput(io.Discard)
			return
		}
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
