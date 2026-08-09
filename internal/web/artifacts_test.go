package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

func artifactWebFixture(t *testing.T, name string, content []byte) (*Server, artifact.Record, string) {
	return artifactWebFixtureWithKind(t, name, content, artifact.KindAuto)
}

func artifactWebFixtureWithKind(t *testing.T, name string, content []byte, kind artifact.Kind) (*Server, artifact.Record, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, name), content, 0o600); err != nil {
		t.Fatal(err)
	}
	recorder, err := session.NewRecorder(workspace, "kimi", "kimi-for-coding")
	if err != nil {
		t.Fatal(err)
	}
	service := artifact.NewService(session.LoadArtifactRecords, time.Now)
	record, err := service.Register(context.Background(), artifact.RegisterRequest{
		SessionID: recorder.UUID(), Workspace: workspace, RelativePath: name, Kind: kind, Focus: true,
	}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	eng := &Engine{taskID: recorder.UUID(), pwd: workspace, recorder: recorder}
	return &Server{Engine: eng, tasks: map[string]*Engine{eng.taskID: eng}, artifacts: service}, record, workspace
}

func managedArtifactWebFixture(t *testing.T) (*Server, artifact.Record, []byte) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	recorder, err := session.NewRecorder(workspace, "provider", "image-model")
	if err != nil {
		t.Fatal(err)
	}
	var pixels bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 40, G: 120, B: 220, A: 255})
	if err := png.Encode(&pixels, img); err != nil {
		t.Fatal(err)
	}
	content := append([]byte(nil), pixels.Bytes()...)
	service := artifact.NewServiceWithManagedRoot(
		session.LoadArtifactRecords, time.Now, filepath.Join(t.TempDir(), "managed"),
	)
	record, err := service.CreateManagedImage(context.Background(), artifact.ManagedImageRequest{
		SessionID: recorder.UUID(), Title: "Generated desk", Reader: bytes.NewReader(content),
		ProviderID: "provider", ModelID: "image-model", OperationID: "operation-1",
		ToolCallID: "call-1", Focus: true, ExpectedMediaType: "image/png",
		ExpectedWidth: 3, ExpectedHeight: 2,
	}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	eng := &Engine{taskID: recorder.UUID(), pwd: workspace, recorder: recorder}
	return &Server{Engine: eng, tasks: map[string]*Engine{eng.taskID: eng}, artifacts: service}, record, content
}

type fakeArtifactSharePublisher struct {
	publishInput cloud.ArtifactShareInput
	publishCalls int
	list         []cloud.ArtifactShareSummary
	revoked      string
}

func (f *fakeArtifactSharePublisher) Publish(_ context.Context, _ *cloud.Credentials, input cloud.ArtifactShareInput) (*cloud.ArtifactShareResult, error) {
	f.publishCalls++
	f.publishInput = input
	return &cloud.ArtifactShareResult{ShareID: "share-1", URL: "https://share.example/s/share-1#k=v1.secret", ExpiresAt: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)}, nil
}

func (f *fakeArtifactSharePublisher) List(_ context.Context, _ *cloud.Credentials, _ string) ([]cloud.ArtifactShareSummary, error) {
	return f.list, nil
}

func (f *fakeArtifactSharePublisher) Revoke(_ context.Context, _ *cloud.Credentials, shareID string) error {
	f.revoked = shareID
	return nil
}

func TestArtifactListAndContentAreTaskScopedAndSecurityHardened(t *testing.T) {
	content := []byte("<h1>isolated</h1>")
	srv, record, _ := artifactWebFixture(t, "report.html", content)

	listW := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+record.SessionID+"/artifacts", nil)
	listReq.SetPathValue("id", record.SessionID)
	srv.handleListArtifacts(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listW.Code, listW.Body.String())
	}
	var records []artifact.Record
	if err := json.Unmarshal(listW.Body.Bytes(), &records); err != nil || len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("records=%+v err=%v", records, err)
	}

	contentW := httptest.NewRecorder()
	contentReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+record.SessionID+"/artifacts/"+record.ID+"/content", nil)
	contentReq.SetPathValue("id", record.SessionID)
	contentReq.SetPathValue("artifactID", record.ID)
	srv.handleArtifactContent(contentW, contentReq)
	if contentW.Code != http.StatusOK || !bytes.Equal(contentW.Body.Bytes(), content) {
		t.Fatalf("content status=%d body=%q", contentW.Code, contentW.Body.String())
	}
	if got := contentW.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff=%q", got)
	}
	if csp := contentW.Header().Get("Content-Security-Policy"); csp == "" || !bytes.Contains([]byte(csp), []byte("connect-src 'none'")) {
		t.Fatalf("csp=%q", csp)
	}

	forgedW := httptest.NewRecorder()
	forgedReq := httptest.NewRequest(http.MethodGet, "/api/tasks/"+record.SessionID+"/artifacts/forged/content", nil)
	forgedReq.SetPathValue("id", record.SessionID)
	forgedReq.SetPathValue("artifactID", "forged")
	srv.handleArtifactContent(forgedW, forgedReq)
	if forgedW.Code != http.StatusNotFound {
		t.Fatalf("forged status=%d body=%s", forgedW.Code, forgedW.Body.String())
	}
}

func TestManagedImageArtifactListContentDownloadDesktopAndSharePolicy(t *testing.T) {
	srv, record, content := managedArtifactWebFixture(t)

	listW := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	listReq.SetPathValue("id", record.SessionID)
	srv.handleListArtifacts(listW, listReq)
	var records []artifact.Record
	if listW.Code != http.StatusOK || json.Unmarshal(listW.Body.Bytes(), &records) != nil ||
		len(records) != 1 || records[0].EffectiveStorageKind() != artifact.StorageManaged {
		t.Fatalf("list status=%d records=%#v body=%s", listW.Code, records, listW.Body.String())
	}

	contentW := httptest.NewRecorder()
	contentReq := httptest.NewRequest(http.MethodGet, "/content", nil)
	contentReq.SetPathValue("id", record.SessionID)
	contentReq.SetPathValue("artifactID", record.ID)
	srv.handleArtifactContent(contentW, contentReq)
	if contentW.Code != http.StatusOK || !bytes.Equal(contentW.Body.Bytes(), content) ||
		contentW.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content status=%d type=%q", contentW.Code, contentW.Header().Get("Content-Type"))
	}

	downloadW := httptest.NewRecorder()
	downloadReq := httptest.NewRequest(http.MethodGet, "/download", nil)
	downloadReq.SetPathValue("id", record.SessionID)
	downloadReq.SetPathValue("artifactID", record.ID)
	srv.handleArtifactDownload(downloadW, downloadReq)
	if downloadW.Code != http.StatusOK ||
		!strings.Contains(downloadW.Header().Get("Content-Disposition"), ".png") {
		t.Fatalf("download status=%d disposition=%q", downloadW.Code, downloadW.Header().Get("Content-Disposition"))
	}

	var revealedPath string
	srv.openArtifact = func(_ context.Context, path string, reveal bool) error {
		if !reveal {
			t.Fatal("generated image card should request Reveal for the desktop action")
		}
		revealedPath = path
		return nil
	}
	revealW := httptest.NewRecorder()
	revealReq := httptest.NewRequest(http.MethodPost, "/reveal", nil)
	revealReq.SetPathValue("id", record.SessionID)
	revealReq.SetPathValue("artifactID", record.ID)
	srv.handleRevealArtifact(revealW, revealReq)
	if revealW.Code != http.StatusNoContent || revealedPath == "" || filepath.Ext(revealedPath) != ".png" {
		t.Fatalf("reveal status=%d path=%q", revealW.Code, revealedPath)
	}

	publisher := &fakeArtifactSharePublisher{}
	srv.artifactShares = publisher
	srv.loadCloudCredentials = func() (*cloud.Credentials, error) {
		return &cloud.Credentials{CloudURL: "https://cloud.example", DeviceToken: "token"}, nil
	}
	shareW := httptest.NewRecorder()
	shareReq := httptest.NewRequest(http.MethodPost, "/share", bytes.NewBufferString(`{}`))
	shareReq.SetPathValue("id", record.SessionID)
	shareReq.SetPathValue("artifactID", record.ID)
	srv.handleCreateArtifactShare(shareW, shareReq)
	if shareW.Code != http.StatusForbidden || publisher.publishCalls != 0 {
		t.Fatalf("share status=%d calls=%d body=%s", shareW.Code, publisher.publishCalls, shareW.Body.String())
	}
}

func TestRemoteTaskExposesOnlyLocalManagedArtifacts(t *testing.T) {
	srv, managed, content := managedArtifactWebFixture(t)
	srv.env = &tools.Env{} // non-local executor boundary
	srv.pwd = "/remote/workspace"

	listW := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	listReq.SetPathValue("id", managed.SessionID)
	srv.handleListArtifacts(listW, listReq)
	var records []artifact.Record
	if listW.Code != http.StatusOK || json.Unmarshal(listW.Body.Bytes(), &records) != nil ||
		len(records) != 1 || records[0].ID != managed.ID ||
		records[0].EffectiveStorageKind() != artifact.StorageManaged {
		t.Fatalf("remote managed list status=%d records=%#v body=%s", listW.Code, records, listW.Body.String())
	}

	contentW := httptest.NewRecorder()
	contentReq := httptest.NewRequest(http.MethodGet, "/content", nil)
	contentReq.SetPathValue("id", managed.SessionID)
	contentReq.SetPathValue("artifactID", managed.ID)
	srv.handleArtifactContent(contentW, contentReq)
	if contentW.Code != http.StatusOK || !bytes.Equal(contentW.Body.Bytes(), content) {
		t.Fatalf("remote managed content status=%d body=%q", contentW.Code, contentW.Body.Bytes())
	}

	workspaceSrv, workspaceRecord, _ := artifactWebFixture(t, "remote-secret.txt", []byte("remote workspace"))
	workspaceSrv.env = &tools.Env{}
	workspaceSrv.pwd = "/remote/workspace"
	workspaceListW := httptest.NewRecorder()
	workspaceListReq := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	workspaceListReq.SetPathValue("id", workspaceRecord.SessionID)
	workspaceSrv.handleListArtifacts(workspaceListW, workspaceListReq)
	records = nil
	if workspaceListW.Code != http.StatusOK || json.Unmarshal(workspaceListW.Body.Bytes(), &records) != nil || len(records) != 0 {
		t.Fatalf("remote workspace list status=%d records=%#v body=%s", workspaceListW.Code, records, workspaceListW.Body.String())
	}
	workspaceContentW := httptest.NewRecorder()
	workspaceContentReq := httptest.NewRequest(http.MethodGet, "/content", nil)
	workspaceContentReq.SetPathValue("id", workspaceRecord.SessionID)
	workspaceContentReq.SetPathValue("artifactID", workspaceRecord.ID)
	workspaceSrv.handleArtifactContent(workspaceContentW, workspaceContentReq)
	if workspaceContentW.Code != http.StatusNotFound || bytes.Contains(workspaceContentW.Body.Bytes(), []byte("remote workspace")) {
		t.Fatalf("remote workspace content status=%d body=%q", workspaceContentW.Code, workspaceContentW.Body.String())
	}
}

func TestArtifactContentRejectsSymlinkSwapAfterRegistration(t *testing.T) {
	srv, record, workspace := artifactWebFixture(t, "report.txt", []byte("safe"))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "report.txt")); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleArtifactContent(w, req)
	if w.Code != http.StatusNotFound || bytes.Contains(w.Body.Bytes(), []byte("SECRET")) {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestArtifactContentRejectsSensitiveInWorkspaceSymlinkSwap(t *testing.T) {
	srv, record, workspace := artifactWebFixture(t, "report.txt", []byte("safe"))
	secret := filepath.Join(workspace, ".env")
	if err := os.WriteFile(secret, []byte("TOKEN=SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(workspace, "report.txt")); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleArtifactContent(w, req)
	if w.Code != http.StatusNotFound || bytes.Contains(w.Body.Bytes(), []byte("SECRET")) {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestArtifactDownloadUsesSafeContentDisposition(t *testing.T) {
	srv, record, _ := artifactWebFixture(t, "report\r\nX-Injected.txt", []byte("safe"))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleArtifactDownload(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	disposition := w.Header().Get("Content-Disposition")
	if strings.ContainsAny(disposition, "\r\n") || w.Header().Get("X-Injected") != "" {
		t.Fatalf("unsafe Content-Disposition: %q", disposition)
	}
}

func TestMarkArtifactViewedIsRevisionScopedAndLeavesOtherArtifactsUnseen(t *testing.T) {
	srv, first, workspace := artifactWebFixture(t, "report.md", []byte("# done"))
	if err := os.WriteFile(filepath.Join(workspace, "second.md"), []byte("# second"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := srv.artifacts.Register(context.Background(), artifact.RegisterRequest{
		SessionID: first.SessionID, Workspace: workspace, RelativePath: "second.md",
		Kind: artifact.KindMarkdown, Focus: false,
	}, srv.recorder)
	if err != nil {
		t.Fatal(err)
	}

	mark := func(record artifact.Record, revision int) *httptest.ResponseRecorder {
		t.Helper()
		body := fmt.Sprintf(`{"artifact_id":%q,"revision":%d}`, record.ID, revision)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+record.SessionID+"/artifacts/viewed", strings.NewReader(body))
		req.SetPathValue("id", record.SessionID)
		srv.handleArtifactsViewed(w, req)
		return w
	}
	if w := mark(first, first.Revision+1); w.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", w.Code, w.Body.String())
	}
	if w := mark(first, first.Revision); w.Code != http.StatusNoContent {
		t.Fatalf("first status=%d body=%s", w.Code, w.Body.String())
	}
	metas, err := session.ListSessions(srv.pwd)
	if err != nil || len(metas) != 1 || !metas[0].ArtifactUnseen ||
		metas[0].ArtifactViewedRevisions[first.ID] != first.Revision ||
		metas[0].ArtifactViewedRevisions[second.ID] != 0 || metas[0].ArtifactViewedAt != "" {
		t.Fatalf("after first metas=%+v err=%v", metas, err)
	}
	if w := mark(second, second.Revision); w.Code != http.StatusNoContent {
		t.Fatalf("second status=%d body=%s", w.Code, w.Body.String())
	}
	metas, err = session.ListSessions(srv.pwd)
	if err != nil || len(metas) != 1 || metas[0].ArtifactUnseen ||
		metas[0].ArtifactViewedRevisions[second.ID] != second.Revision {
		t.Fatalf("after second metas=%+v err=%v", metas, err)
	}
}

func TestArtifactShareRequiresLoginWithoutCallingCloud(t *testing.T) {
	srv, record, _ := artifactWebFixture(t, "report.md", []byte("# private"))
	publisher := &fakeArtifactSharePublisher{}
	srv.artifactShares = publisher
	srv.loadCloudCredentials = func() (*cloud.Credentials, error) { return nil, nil }
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/share", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleCreateArtifactShare(w, req)
	if w.Code != http.StatusUnauthorized || publisher.publishCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, publisher.publishCalls, w.Body.String())
	}
}

func TestArtifactSharePublishesAnImmutableTaskScopedSnapshot(t *testing.T) {
	content := []byte("# final result")
	srv, record, _ := artifactWebFixture(t, "report.md", content)
	publisher := &fakeArtifactSharePublisher{}
	srv.artifactShares = publisher
	srv.loadCloudCredentials = func() (*cloud.Credentials, error) {
		return &cloud.Credentials{CloudURL: "https://cloud.example", DeviceToken: "token"}, nil
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/share", bytes.NewBufferString(`{"expires_in_seconds":3600}`))
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleCreateArtifactShare(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if publisher.publishCalls != 1 || !bytes.Equal(publisher.publishInput.Content, content) ||
		publisher.publishInput.ArtifactID != record.ID || publisher.publishInput.Revision != record.Revision ||
		publisher.publishInput.RelativePath != record.RelativePath || publisher.publishInput.ExpiresIn != time.Hour {
		t.Fatalf("publish input = %#v", publisher.publishInput)
	}
	if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "#k=v1.secret") {
		t.Fatalf("headers=%v body=%s", w.Header(), w.Body.String())
	}
}

func TestArtifactShareRevokeIsScopedToTheSelectedArtifact(t *testing.T) {
	srv, record, _ := artifactWebFixture(t, "report.md", []byte("done"))
	publisher := &fakeArtifactSharePublisher{list: []cloud.ArtifactShareSummary{{ShareID: "different-share", ArtifactID: record.ID}}}
	srv.artifactShares = publisher
	srv.loadCloudCredentials = func() (*cloud.Credentials, error) {
		return &cloud.Credentials{CloudURL: "https://cloud.example", DeviceToken: "token"}, nil
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/share", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	req.SetPathValue("shareID", "forged-share")
	srv.handleRevokeArtifactShare(w, req)
	if w.Code != http.StatusNotFound || publisher.revoked != "" {
		t.Fatalf("status=%d revoked=%q body=%s", w.Code, publisher.revoked, w.Body.String())
	}
}

type changingSnapshotFile struct {
	reader *bytes.Reader
	stats  int
}

func (f *changingSnapshotFile) Read(p []byte) (int, error) { return f.reader.Read(p) }
func (f *changingSnapshotFile) Seek(offset int64, whence int) (int64, error) {
	return f.reader.Seek(offset, whence)
}
func (f *changingSnapshotFile) Stat() (os.FileInfo, error) {
	f.stats++
	return snapshotFileInfo{size: int64(f.reader.Len()), mod: time.Unix(int64(f.stats), 0)}, nil
}

type snapshotFileInfo struct {
	size int64
	mod  time.Time
}

type sameStatChangingSnapshotFile struct {
	reader *bytes.Reader
	first  []byte
	second []byte
}

func (f *sameStatChangingSnapshotFile) Read(p []byte) (int, error) { return f.reader.Read(p) }
func (f *sameStatChangingSnapshotFile) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekStart {
		f.reader = bytes.NewReader(f.second)
	}
	return f.reader.Seek(offset, whence)
}
func (f *sameStatChangingSnapshotFile) Stat() (os.FileInfo, error) {
	return snapshotFileInfo{size: int64(len(f.first)), mod: time.Unix(1, 0)}, nil
}

func (f snapshotFileInfo) Name() string       { return "artifact" }
func (f snapshotFileInfo) Size() int64        { return f.size }
func (f snapshotFileInfo) Mode() os.FileMode  { return 0o600 }
func (f snapshotFileInfo) ModTime() time.Time { return f.mod }
func (f snapshotFileInfo) IsDir() bool        { return false }
func (f snapshotFileInfo) Sys() any           { return nil }

func TestReadArtifactSnapshotRejectsAFileChangedDuringRead(t *testing.T) {
	file := &changingSnapshotFile{reader: bytes.NewReader([]byte("changing"))}
	_, err := readArtifactSnapshot(file, artifact.MaxShareSize)
	if !errors.Is(err, errArtifactChanged) {
		t.Fatalf("readArtifactSnapshot error = %v", err)
	}
}

func TestReadArtifactSnapshotRejectsSameSizeAndTimestampRewrite(t *testing.T) {
	file := &sameStatChangingSnapshotFile{
		first: []byte("version-one"), second: []byte("version-two"), reader: bytes.NewReader([]byte("version-one")),
	}
	_, err := readArtifactSnapshot(file, artifact.MaxShareSize)
	if !errors.Is(err, errArtifactChanged) {
		t.Fatalf("readArtifactSnapshot error = %v", err)
	}
}

func TestDesktopArtifactActionsResolveOnlyRegisteredTaskArtifacts(t *testing.T) {
	srv, record, workspace := artifactWebFixture(t, "report.txt", []byte("done"))
	var openedPath string
	var revealed bool
	srv.openArtifact = func(_ context.Context, path string, reveal bool) error {
		openedPath, revealed = path, reveal
		return nil
	}
	wantPath, err := filepath.EvalSymlinks(filepath.Join(workspace, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/open", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleOpenArtifact(w, req)
	if w.Code != http.StatusNoContent || openedPath != wantPath || revealed {
		t.Fatalf("status=%d path=%q reveal=%v", w.Code, openedPath, revealed)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/reveal", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleRevealArtifact(w, req)
	if w.Code != http.StatusNoContent || !revealed {
		t.Fatalf("status=%d reveal=%v", w.Code, revealed)
	}

	openedPath = ""
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/open", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", "forged")
	srv.handleOpenArtifact(w, req)
	if w.Code != http.StatusNotFound || openedPath != "" {
		t.Fatalf("forged status=%d path=%q", w.Code, openedPath)
	}
}

func TestDesktopArtifactOpenRejectsActiveHTMLButRevealRemainsAvailable(t *testing.T) {
	srv, record, _ := artifactWebFixture(t, "report.html", []byte("<script>alert(1)</script>"))
	called := false
	srv.openArtifact = func(_ context.Context, _ string, _ bool) error {
		called = true
		return nil
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/open", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleOpenArtifact(w, req)
	if w.Code != http.StatusForbidden || called {
		t.Fatalf("open status=%d called=%v", w.Code, called)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/reveal", nil)
	req.SetPathValue("id", record.SessionID)
	req.SetPathValue("artifactID", record.ID)
	srv.handleRevealArtifact(w, req)
	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("reveal status=%d called=%v", w.Code, called)
	}
}

func TestDesktopArtifactOpenRejectsSpoofedActiveAndExecutableFiles(t *testing.T) {
	tests := []struct {
		name string
		file string
		kind artifact.Kind
	}{
		{name: "html kind downgrade", file: "report.html", kind: artifact.KindText},
		{name: "svg active document", file: "diagram.svg", kind: artifact.KindImage},
		{name: "mac command", file: "report.command", kind: artifact.KindText},
		{name: "windows script host", file: "report.js", kind: artifact.KindCode},
		{name: "windows batch", file: "report.bat", kind: artifact.KindCode},
		{name: "linux launcher", file: "report.desktop", kind: artifact.KindText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, record, _ := artifactWebFixtureWithKind(t, tt.file, []byte("active"), tt.kind)
			called := false
			srv.openArtifact = func(_ context.Context, _ string, _ bool) error {
				called = true
				return nil
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/open", nil)
			req.SetPathValue("id", record.SessionID)
			req.SetPathValue("artifactID", record.ID)
			srv.handleOpenArtifact(w, req)
			if w.Code != http.StatusForbidden || called {
				t.Fatalf("file=%q kind=%q status=%d called=%v", tt.file, tt.kind, w.Code, called)
			}
		})
	}
}
