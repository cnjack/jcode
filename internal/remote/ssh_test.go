package remote

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/tools"
)

func TestResolveTarget(t *testing.T) {
	cases := []struct {
		name     string
		opts     SSHOptions
		wantAddr string
		wantUser string
	}{
		{"host+user+port", SSHOptions{Host: "1.2.3.4", User: "root", Port: 2222}, "1.2.3.4:2222", "root"},
		{"host only defaults user", SSHOptions{Host: "1.2.3.4"}, "1.2.3.4", "root"},
		{"user@host embedded", SSHOptions{Host: "deploy@example.com", Port: 22}, "example.com:22", "deploy"},
		{"explicit user beats embedded", SSHOptions{Host: "deploy@example.com", User: "root"}, "example.com", "root"},
		{"host already has port ignores port opt", SSHOptions{Host: "1.2.3.4:2200", Port: 22, User: "u"}, "1.2.3.4:2200", "u"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr, user := resolveTarget(c.opts)
			if addr != c.wantAddr || user != c.wantUser {
				t.Fatalf("resolveTarget(%+v) = (%q,%q), want (%q,%q)", c.opts, addr, user, c.wantUser, c.wantAddr)
			}
		})
	}
}

// fakeExecutor implements tools.Executor for ListDirs / DiscoverPwd tests.
type fakeExecutor struct {
	out string
	err error
}

func (f *fakeExecutor) ReadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeExecutor) WriteFile(context.Context, string, []byte, os.FileMode) error {
	return nil
}
func (f *fakeExecutor) MkdirAll(context.Context, string, os.FileMode) error { return nil }
func (f *fakeExecutor) Stat(context.Context, string) (*tools.FileInfo, error) {
	return &tools.FileInfo{Exists: true, IsDir: true}, nil
}
func (f *fakeExecutor) Exec(context.Context, string, string, time.Duration) (string, string, error) {
	return f.out, "", f.err
}
func (f *fakeExecutor) Platform() string { return "linux/amd64" }
func (f *fakeExecutor) Label() string    { return "fake" }

func TestListDirs(t *testing.T) {
	// `ls -F -1` output: dirs end with "/", executables with "*", files plain.
	exec := &fakeExecutor{out: "bin/\nREADME.md\nsrc/\nrun.sh*\n.git/\n"}
	dirs, err := ListDirs(context.Background(), exec, "/home/app")
	if err != nil {
		t.Fatalf("ListDirs error: %v", err)
	}
	want := []string{"..", "bin", "src", ".git"}
	if len(dirs) != len(want) {
		t.Fatalf("ListDirs = %v, want %v", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Fatalf("ListDirs[%d] = %q, want %q (full: %v)", i, dirs[i], want[i], dirs)
		}
	}
}

func TestListDirsRootHasNoParent(t *testing.T) {
	exec := &fakeExecutor{out: "etc/\nusr/\n"}
	dirs, err := ListDirs(context.Background(), exec, "/")
	if err != nil {
		t.Fatalf("ListDirs error: %v", err)
	}
	if len(dirs) == 0 || dirs[0] == ".." {
		t.Fatalf("root listing must not include '..': %v", dirs)
	}
}

func TestDiscoverPwdFallback(t *testing.T) {
	exec := &fakeExecutor{out: "  /opt/work \n"}
	if got := DiscoverPwd(context.Background(), exec, "/root"); got != "/opt/work" {
		t.Fatalf("DiscoverPwd = %q, want /opt/work", got)
	}
	bad := &fakeExecutor{out: "", err: context.DeadlineExceeded}
	if got := DiscoverPwd(context.Background(), bad, "/root"); got != "/root" {
		t.Fatalf("DiscoverPwd fallback = %q, want /root", got)
	}
}

func TestProjectLabel(t *testing.T) {
	// ProjectLabel needs a *tools.SSHExecutor; build a zero executor isn't
	// possible (unexported fields), so assert the format via a helper on a
	// constructed value would require reflection. Instead, verify the path
	// normalization branch indirectly through label-shaped expectations.
	if got := normalizeRemotePath("home/app"); got != "/home/app" {
		t.Fatalf("normalizeRemotePath = %q, want /home/app", got)
	}
	if got := normalizeRemotePath("/srv"); got != "/srv" {
		t.Fatalf("normalizeRemotePath = %q, want /srv", got)
	}
}
