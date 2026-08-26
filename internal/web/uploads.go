package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

const (
	maxTaskUploadBytes   int64 = 100 << 20
	multipartOverheadCap int64 = 1 << 20
	localUploadDirMode         = 0o700
	uploadFileMode             = 0o600
)

type taskUploadResponse struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (s *Server) handleTaskUpload(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if err := session.ValidateSessionID(taskID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}
	eng := s.resolveEngine(taskID)
	if eng == nil || eng.env == nil || eng.env.Exec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task is not available"})
		return
	}
	if eng.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "wait for the current turn to finish before uploading files"})
		return
	}

	name, data, err := readTaskUpload(w, r, maxTaskUploadBytes)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || errors.Is(err, errUploadTooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file exceeds the 100MB upload limit"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	safeName, err := uniqueUploadName(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not allocate upload filename"})
		return
	}
	dir, remote := taskUploadDir(eng, taskID)
	if err := prepareUploadDir(r, eng, dir, remote); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not prepare task upload directory"})
		return
	}
	target := joinUploadPath(dir, safeName, remote)
	if err := eng.env.Exec.WriteFile(r.Context(), target, data, uploadFileMode); err != nil {
		config.Logger().Printf("[web] task upload write failed for %s: %v", taskID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save uploaded file"})
		return
	}
	writeJSON(w, http.StatusCreated, taskUploadResponse{Path: target, Name: safeName, Size: int64(len(data))})
}

var errUploadTooLarge = errors.New("task upload exceeds size limit")

func readTaskUpload(w http.ResponseWriter, r *http.Request, maxBytes int64) (string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+multipartOverheadCap)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return "", nil, fmt.Errorf("parse multipart upload: %w", err)
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", nil, errors.New("multipart field \"file\" is required")
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("read uploaded file: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return "", nil, errUploadTooLarge
	}
	return header.Filename, data, nil
}

func taskUploadDir(eng *Engine, taskID string) (string, bool) {
	if eng != nil && eng.env != nil && eng.env.IsRemote() {
		return pathpkg.Join("/tmp", ".jcode-uploads-"+taskID), true
	}
	return filepath.Join(config.ConfigDir(), "uploads", taskID), false
}

func prepareUploadDir(r *http.Request, eng *Engine, dir string, remote bool) error {
	if !remote {
		if err := os.MkdirAll(dir, localUploadDirMode); err != nil {
			return err
		}
		return os.Chmod(dir, localUploadDirMode)
	}
	if err := eng.env.Exec.MkdirAll(r.Context(), dir, localUploadDirMode); err != nil {
		return err
	}
	_, stderr, err := eng.env.Exec.Exec(r.Context(), "chmod 700 "+tools.ShellQuote(dir), "", 10*time.Second)
	if err != nil {
		return fmt.Errorf("secure remote upload directory: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

func joinUploadPath(dir, name string, remote bool) string {
	if remote {
		return pathpkg.Join(dir, name)
	}
	return filepath.Join(dir, name)
}

func uniqueUploadName(original string) (string, error) {
	name := sanitizeUploadName(original)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	var entropy [3]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return stem + "-" + hex.EncodeToString(entropy[:]) + ext, nil
}

func sanitizeUploadName(name string) string {
	name = pathpkg.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), strings.ContainsRune("-_. ", r):
			b.WriteRune(r)
		case unicode.IsControl(r):
			continue
		default:
			b.WriteByte('_')
		}
	}
	name = strings.TrimSpace(strings.TrimLeft(b.String(), "."))
	if name == "" {
		return "attachment"
	}
	runes := []rune(name)
	if len(runes) > 120 {
		extRunes := []rune(filepath.Ext(name))
		if len(extRunes) > 20 {
			extRunes = extRunes[:20]
		}
		keep := 120 - len(extRunes)
		name = string(runes[:keep]) + string(extRunes)
	}
	return name
}

func removeLocalTaskUploads(taskID string) {
	if session.ValidateSessionID(taskID) != nil {
		return
	}
	_ = os.RemoveAll(filepath.Join(config.ConfigDir(), "uploads", taskID))
}
