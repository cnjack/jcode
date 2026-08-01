package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/config"
)

func (s *Server) handleOpenArtifact(w http.ResponseWriter, r *http.Request) {
	s.handleArtifactDesktopAction(w, r, false)
}

func (s *Server) handleRevealArtifact(w http.ResponseWriter, r *http.Request) {
	s.handleArtifactDesktopAction(w, r, true)
}

func (s *Server) handleArtifactDesktopAction(w http.ResponseWriter, r *http.Request, reveal bool) {
	if s.artifacts == nil || s.openArtifact == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	sessionID, workspace, err := s.artifactWorkspace(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	record, absolutePath, err := s.artifacts.Resolve(r.Context(), sessionID, workspace, r.PathValue("artifactID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if !reveal && !artifactHostOpenAllowed(record) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "active or executable artifacts can only be previewed in the sandboxed viewer"})
		return
	}
	if err := s.openArtifact(r.Context(), absolutePath, reveal); err != nil {
		config.Logger().Printf("[artifact] desktop action reveal=%v path=%s: %v", reveal, absolutePath, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "desktop artifact action failed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

var blockedArtifactHostOpenExtensions = map[string]struct{}{
	".app": {}, ".application": {}, ".bat": {}, ".cmd": {}, ".com": {}, ".command": {},
	".cpl": {}, ".desktop": {}, ".exe": {}, ".gadget": {}, ".hta": {}, ".htm": {}, ".html": {},
	".inf": {}, ".ins": {}, ".isp": {}, ".jar": {}, ".js": {}, ".jse": {}, ".lnk": {}, ".msc": {},
	".msi": {}, ".msp": {}, ".mst": {}, ".pif": {}, ".ps1": {}, ".reg": {}, ".scr": {}, ".sh": {},
	".svg": {}, ".svgz": {}, ".url": {}, ".vb": {}, ".vbe": {}, ".vbs": {}, ".workflow": {},
	".ws": {}, ".wsf": {}, ".wsh": {}, ".xhtml": {},
}

func artifactHostOpenAllowed(record artifact.Record) bool {
	if record.Kind == artifact.KindHTML {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(record.MediaType, ";")[0]))
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" || mediaType == "image/svg+xml" {
		return false
	}
	_, blocked := blockedArtifactHostOpenExtensions[strings.ToLower(filepath.Ext(record.RelativePath))]
	return !blocked
}

func openArtifactOnDesktop(ctx context.Context, path string, reveal bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect desktop artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("desktop artifact must remain a regular non-symlink file")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		args := []string{path}
		if reveal {
			args = []string{"-R", path}
		}
		command = exec.CommandContext(ctx, "open", args...)
	case "windows":
		if reveal {
			command = exec.CommandContext(ctx, "explorer.exe", "/select,"+path)
		} else {
			command = exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", path)
		}
	default:
		target := path
		if reveal {
			target = filepath.Dir(path)
		}
		command = exec.CommandContext(ctx, "xdg-open", target)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (%s)", command.Path, err, strings.TrimSpace(string(output)))
	}
	return nil
}
