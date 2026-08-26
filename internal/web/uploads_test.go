package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/tools"
)

func taskUploadRequest(t *testing.T, taskID, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/uploads", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.SetPathValue("id", taskID)
	return req
}

func TestTaskUploadStoresManagedLocalCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	env := tools.NewEnv(workspace, "darwin/arm64")
	eng := &Engine{taskID: "task-a", pwd: workspace, env: env}
	s := &Server{Engine: eng, tasks: map[string]*Engine{eng.taskID: eng}}

	rec := httptest.NewRecorder()
	s.handleTaskUpload(rec, taskUploadRequest(t, eng.taskID, `../../report?.pdf`, []byte("pdf-data")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var response taskUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(home, ".jcode", "uploads", eng.taskID)
	if filepath.Dir(response.Path) != wantDir || strings.Contains(response.Name, "..") || strings.ContainsAny(response.Name, `/\\?`) {
		t.Fatalf("unsafe upload response: %#v", response)
	}
	got, err := os.ReadFile(response.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "pdf-data" || response.Size != int64(len(got)) {
		t.Fatalf("content=%q size=%d", got, response.Size)
	}
	if info, err := os.Stat(response.Path); err != nil || info.Mode().Perm() != uploadFileMode {
		t.Fatalf("file mode: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(wantDir); err != nil || info.Mode().Perm() != localUploadDirMode {
		t.Fatalf("dir mode: info=%v err=%v", info, err)
	}
}

func TestReadTaskUploadEnforcesLimit(t *testing.T) {
	req := taskUploadRequest(t, "task-a", "large.bin", []byte("12345"))
	rec := httptest.NewRecorder()
	_, _, err := readTaskUpload(rec, req, 4)
	if !errors.Is(err, errUploadTooLarge) {
		t.Fatalf("err=%v, want errUploadTooLarge", err)
	}
}

func TestSanitizeUploadNameBoundsAndNormalizes(t *testing.T) {
	name := sanitizeUploadName("../../." + strings.Repeat("a", 140) + "." + strings.Repeat("x", 40))
	if len([]rune(name)) != 120 || strings.ContainsAny(name, `/\\`) || strings.HasPrefix(name, ".") {
		t.Fatalf("name=%q runes=%d", name, len([]rune(name)))
	}
}

func TestRemoveLocalTaskUploadsIsTaskScoped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	taskDir := filepath.Join(home, ".jcode", "uploads", "task-a")
	otherDir := filepath.Join(home, ".jcode", "uploads", "task-b")
	for _, dir := range []string{taskDir, otherDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removeLocalTaskUploads("task-a")
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatalf("task upload dir still exists: %v", err)
	}
	if _, err := os.Stat(otherDir); err != nil {
		t.Fatalf("other task upload dir removed: %v", err)
	}
}

type uploadRemoteExecutor struct {
	mkdirPath string
	writePath string
	writeData []byte
	chmodCmd  string
}

func (*uploadRemoteExecutor) ReadFile(context.Context, string) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (e *uploadRemoteExecutor) WriteFile(_ context.Context, path string, data []byte, _ os.FileMode) error {
	e.writePath = path
	e.writeData = append([]byte(nil), data...)
	return nil
}
func (e *uploadRemoteExecutor) MkdirAll(_ context.Context, path string, _ os.FileMode) error {
	e.mkdirPath = path
	return nil
}
func (*uploadRemoteExecutor) Stat(context.Context, string) (*tools.FileInfo, error) {
	return &tools.FileInfo{}, nil
}
func (e *uploadRemoteExecutor) Exec(_ context.Context, command, _ string, _ time.Duration) (string, string, error) {
	e.chmodCmd = command
	return "", "", nil
}
func (*uploadRemoteExecutor) Platform() string { return "linux/amd64" }
func (*uploadRemoteExecutor) Label() string    { return "remote-test" }
func (*uploadRemoteExecutor) Probe(context.Context) error {
	return nil
}
func (*uploadRemoteExecutor) Close() error { return nil }
func (*uploadRemoteExecutor) ProjectLabel(pwd string) string {
	return "ssh://example" + pwd
}

func TestTaskUploadWritesIntoRemoteExecutionEnvironment(t *testing.T) {
	executor := &uploadRemoteExecutor{}
	env := tools.NewEnv(t.TempDir(), "darwin/arm64")
	env.SetRemote(executor, "/srv/project")
	eng := &Engine{taskID: "task-remote", pwd: "/srv/project", env: env}
	s := &Server{Engine: eng, tasks: map[string]*Engine{eng.taskID: eng}}

	rec := httptest.NewRecorder()
	s.handleTaskUpload(rec, taskUploadRequest(t, eng.taskID, "notes.txt", []byte("hello")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	wantDir := "/tmp/.jcode-uploads-" + eng.taskID
	if executor.mkdirPath != wantDir || filepath.Dir(executor.writePath) != wantDir {
		t.Fatalf("mkdir=%q write=%q", executor.mkdirPath, executor.writePath)
	}
	if string(executor.writeData) != "hello" || executor.chmodCmd != "chmod 700 "+tools.ShellQuote(wantDir) {
		t.Fatalf("data=%q chmod=%q", executor.writeData, executor.chmodCmd)
	}
}

func TestTaskUploadRejectsRunningTask(t *testing.T) {
	env := tools.NewEnv(t.TempDir(), "darwin/arm64")
	eng := &Engine{taskID: "task-a", pwd: env.Pwd(), env: env}
	eng.running.Store(true)
	s := &Server{Engine: eng, tasks: map[string]*Engine{eng.taskID: eng}}

	rec := httptest.NewRecorder()
	s.handleTaskUpload(rec, taskUploadRequest(t, eng.taskID, "notes.txt", []byte("hello")))
	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}
