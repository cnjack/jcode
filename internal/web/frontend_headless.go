//go:build jcode_headless

package web

import (
	"net/http"
)

// Headless build (desktop sidecar, `-tags jcode_headless`): the SPA is NOT
// embedded — the Tauri shell serves the page itself and reaches the API over an
// absolute loopback URL. There is no dist/ to embed, so this file intentionally
// omits `//go:embed` and replaces the SPA handler with a fixed 404 responder
// (a direct browser hit on the headless server has no page to render). This
// keeps the package compiling without a dist/ directory present.
//
// frontend.go (the embedded variant) is excluded under this tag, so the symbols
// it defines (distFS, frontendFS, newSPAHandler, spaHandler) are redefined here
// to keep the rest of the server linking unchanged.

// distFS is unused in headless builds; declared only for symbol parity.
var distFS = emptyFS{}

type emptyFS struct{}

// frontendFS returns no file system in headless builds.
func frontendFS() http.FileSystem { return nil }

// newSPAHandler returns a 404 responder for every path. A direct browser hit on
// the headless server has no embedded page to render; the real UI lives in the
// Tauri shell. API routes are registered separately and never reach here.
func newSPAHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "headless build: open the jcode desktop app", http.StatusNotFound)
	})
}
