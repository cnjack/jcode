package tools

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
)

type failingFileMetadataExecutor struct {
	Executor
	statInfo   *FileInfo
	statErr    error
	readErr    error
	writeCalls int
	mkdirCalls int
}

func (e *failingFileMetadataExecutor) Stat(context.Context, string) (*FileInfo, error) {
	return e.statInfo, e.statErr
}

func (e *failingFileMetadataExecutor) ReadFile(context.Context, string) ([]byte, error) {
	return nil, e.readErr
}

func (e *failingFileMetadataExecutor) WriteFile(context.Context, string, []byte, os.FileMode) error {
	e.writeCalls++
	return nil
}

func (e *failingFileMetadataExecutor) MkdirAll(context.Context, string, os.FileMode) error {
	e.mkdirCalls++
	return nil
}

func fatalMetadataError() error {
	return Fatal(&RemoteTransportError{
		Kind: "ssh", Code: "ssh_connection_failed", Phase: RemoteTransportBeforeDispatch,
		Retryable: true, Err: errors.New("connection lost"),
	})
}

func TestWriteFailsClosedWhenRemoteStatFails(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	exec := &failingFileMetadataExecutor{Executor: env.Exec, statErr: fatalMetadataError()}
	env.Exec = exec

	_, err := env.NewWriteTool().InvokableRun(
		context.Background(), `{"file_path":"target.txt","content":"replacement"}`,
	)
	if !IsFatal(err) {
		t.Fatalf("write error = %v, want Fatal remote transport error", err)
	}
	if exec.writeCalls != 0 {
		t.Fatalf("write dispatched %d times after unknown stat outcome", exec.writeCalls)
	}
}

func TestWriteFailsClosedWhenRemoteBackupReadFails(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	exec := &failingFileMetadataExecutor{
		Executor: env.Exec, statInfo: &FileInfo{Exists: true}, readErr: fatalMetadataError(),
	}
	env.Exec = exec

	_, err := env.NewWriteTool().InvokableRun(
		context.Background(), `{"file_path":"target.txt","content":"replacement"}`,
	)
	if !IsFatal(err) {
		t.Fatalf("write error = %v, want Fatal remote transport error", err)
	}
	if exec.writeCalls != 0 {
		t.Fatalf("write dispatched %d times after failed existing-file read", exec.writeCalls)
	}
}

func TestEditCreateFailsClosedWhenRemoteStatFails(t *testing.T) {
	env := NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH)
	exec := &failingFileMetadataExecutor{Executor: env.Exec, statErr: fatalMetadataError()}
	env.Exec = exec

	_, err := env.NewEditTool().InvokableRun(
		context.Background(), `{"file_path":"target.txt","old_string":"","new_string":"new"}`,
	)
	if !IsFatal(err) {
		t.Fatalf("edit create error = %v, want Fatal remote transport error", err)
	}
	if exec.mkdirCalls != 0 || exec.writeCalls != 0 {
		t.Fatalf("edit create mutated after unknown stat: mkdir=%d write=%d", exec.mkdirCalls, exec.writeCalls)
	}
}
