package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
)

var errArtifactChanged = errors.New("artifact changed while creating snapshot")

type artifactSnapshotFile interface {
	io.ReadSeeker
	Stat() (os.FileInfo, error)
}

func (s *Server) artifactWorkspace(r *http.Request) (string, string, error) {
	sessionID := r.PathValue("id")
	if err := session.ValidateSessionID(sessionID); err != nil {
		return "", "", err
	}
	if eng := s.resolveEngine(sessionID); eng != nil && eng.env != nil && eng.env.IsRemote() {
		return "", "", fmt.Errorf("remote artifacts are not supported")
	}
	workspace, err := s.workspacePwdForTask(sessionID)
	if err != nil {
		return "", "", err
	}
	if workspace == "" {
		return "", "", os.ErrNotExist
	}
	return sessionID, workspace, nil
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	if s.artifacts == nil {
		writeJSON(w, http.StatusOK, []artifact.Record{})
		return
	}
	sessionID, workspace, err := s.artifactWorkspace(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task artifacts not found"})
		return
	}
	records, err := s.artifacts.List(r.Context(), sessionID, workspace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load task artifacts"})
		return
	}
	if records == nil {
		records = []artifact.Record{}
	}
	writeJSON(w, http.StatusOK, records)
}

func artifactInlineLimit(kind artifact.Kind) int64 {
	switch kind {
	case artifact.KindText, artifact.KindMarkdown, artifact.KindCode, artifact.KindHTML, artifact.KindCSV:
		return artifact.MaxInlineTextSize
	default:
		return artifact.MaxInlineBinarySize
	}
}

func setArtifactContentHeaders(w http.ResponseWriter, record artifact.Record) {
	w.Header().Set("Content-Type", record.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; sandbox")
}

func (s *Server) handleArtifactContent(w http.ResponseWriter, r *http.Request) {
	s.serveArtifactFile(w, r, false)
}

func (s *Server) handleArtifactDownload(w http.ResponseWriter, r *http.Request) {
	s.serveArtifactFile(w, r, true)
}

func (s *Server) serveArtifactFile(w http.ResponseWriter, r *http.Request, download bool) {
	if s.artifacts == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	sessionID, workspace, err := s.artifactWorkspace(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	record, file, err := s.artifacts.Open(r.Context(), sessionID, workspace, r.PathValue("artifactID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	defer func() { _ = file.Close() }()
	limit := artifactInlineLimit(record.Kind)
	if download {
		limit = artifact.MaxDownloadSize
	}
	if record.Size > limit {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "artifact_too_large"})
		return
	}
	setArtifactContentHeaders(w, record)
	name := artifactDownloadName(record.RelativePath)
	if download {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	}
	http.ServeContent(w, r, name, record.UpdatedAt, file)
}

func artifactDownloadName(relativePath string) string {
	base := filepath.Base(relativePath)
	var safe strings.Builder
	for _, char := range base {
		if char < 0x20 || char == 0x7f || char == '"' || char == '\\' || char == '/' {
			safe.WriteByte('_')
			continue
		}
		safe.WriteRune(char)
	}
	name := strings.TrimSpace(safe.String())
	if name == "" || name == "." {
		return "artifact"
	}
	return name
}

func (s *Server) handleArtifactsViewed(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if err := session.ValidateSessionID(sessionID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}
	meta, err := session.UpdateSessionMeta(sessionID, func(meta *session.SessionMeta) {
		meta.ArtifactViewedAt = meta.ArtifactUpdatedAt
		if meta.ArtifactViewedAt == "" {
			meta.ArtifactViewedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		meta.ArtifactUnseen = false
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update artifact view state"})
		return
	}
	if meta == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readArtifactSnapshot(file artifactSnapshotFile, limit int64) ([]byte, error) {
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() > limit {
		return nil, artifact.ErrTooLarge
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, artifact.ErrTooLarge
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, errArtifactChanged
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	verification, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(content, verification) {
		return nil, errArtifactChanged
	}
	return content, nil
}

func (s *Server) cloudArtifactCredentials(w http.ResponseWriter) (*cloud.Credentials, bool) {
	if s.loadCloudCredentials == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "cloud login is required"})
		return nil, false
	}
	creds, err := s.loadCloudCredentials()
	if err != nil {
		config.Logger().Printf("[artifact-share] load credentials: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cloud credentials are unavailable"})
		return nil, false
	}
	if creds == nil || strings.TrimSpace(creds.DeviceToken) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "cloud login is required"})
		return nil, false
	}
	return creds, true
}

func decodeArtifactShareRequest(r *http.Request) (time.Duration, error) {
	var req struct {
		ExpiresInSeconds int64 `json:"expires_in_seconds,omitempty"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return 0, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("request body must contain one JSON object")
	}
	if req.ExpiresInSeconds == 0 {
		req.ExpiresInSeconds = int64((7 * 24 * time.Hour) / time.Second)
	}
	if req.ExpiresInSeconds < int64(time.Hour/time.Second) || req.ExpiresInSeconds > int64((30*24*time.Hour)/time.Second) {
		return 0, fmt.Errorf("expiry must be between 1 hour and 30 days")
	}
	return time.Duration(req.ExpiresInSeconds) * time.Second, nil
}

func (s *Server) handleCreateArtifactShare(w http.ResponseWriter, r *http.Request) {
	if s.artifacts == nil || s.artifactShares == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "artifact sharing is unavailable"})
		return
	}
	creds, ok := s.cloudArtifactCredentials(w)
	if !ok {
		return
	}
	expiresIn, err := decodeArtifactShareRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artifact share request"})
		return
	}
	sessionID, workspace, err := s.artifactWorkspace(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	record, file, err := s.artifacts.Open(r.Context(), sessionID, workspace, r.PathValue("artifactID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	defer func() { _ = file.Close() }()
	content, err := readArtifactSnapshot(file, artifact.MaxShareSize)
	if errors.Is(err, artifact.ErrTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "artifact_too_large"})
		return
	}
	if errors.Is(err, errArtifactChanged) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "artifact_changed"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not snapshot artifact"})
		return
	}
	result, err := s.artifactShares.Publish(r.Context(), creds, cloud.ArtifactShareInput{
		ArtifactID: record.ID, Revision: record.Revision, Title: record.Title,
		RelativePath: record.RelativePath, MediaType: record.MediaType, Kind: string(record.Kind),
		Content: content, ExpiresIn: expiresIn,
	})
	if err != nil {
		config.Logger().Printf("[artifact-share] publish %s r%d: %v", record.ID, record.Revision, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not share artifact"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) artifactRecord(r *http.Request) (artifact.Record, error) {
	if s.artifacts == nil {
		return artifact.Record{}, os.ErrNotExist
	}
	sessionID, workspace, err := s.artifactWorkspace(r)
	if err != nil {
		return artifact.Record{}, err
	}
	records, err := s.artifacts.List(r.Context(), sessionID, workspace)
	if err != nil {
		return artifact.Record{}, err
	}
	for _, record := range records {
		if record.ID == r.PathValue("artifactID") {
			return record, nil
		}
	}
	return artifact.Record{}, os.ErrNotExist
}

func (s *Server) handleListArtifactShares(w http.ResponseWriter, r *http.Request) {
	record, err := s.artifactRecord(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	creds, ok := s.cloudArtifactCredentials(w)
	if !ok {
		return
	}
	shares, err := s.artifactShares.List(r.Context(), creds, record.ID)
	if err != nil {
		config.Logger().Printf("[artifact-share] list %s: %v", record.ID, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not load artifact shares"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleRevokeArtifactShare(w http.ResponseWriter, r *http.Request) {
	record, err := s.artifactRecord(r)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	creds, ok := s.cloudArtifactCredentials(w)
	if !ok {
		return
	}
	shares, err := s.artifactShares.List(r.Context(), creds, record.ID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not verify artifact share"})
		return
	}
	shareID := r.PathValue("shareID")
	owned := false
	for _, share := range shares {
		if share.ShareID == shareID && share.ArtifactID == record.ID {
			owned = true
			break
		}
	}
	if !owned {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact share not found"})
		return
	}
	if err := s.artifactShares.Revoke(r.Context(), creds, shareID); err != nil {
		config.Logger().Printf("[artifact-share] revoke %s: %v", shareID, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not revoke artifact share"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
