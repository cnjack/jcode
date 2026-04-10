package tools

import "testing"

func TestDetectBinaryByExtension(t *testing.T) {
	tests := []struct {
		path   string
		binary bool
	}{
		{"/foo/bar.png", true},
		{"/foo/bar.PNG", true},
		{"/foo/bar.exe", true},
		{"/foo/bar.dll", true},
		{"/foo/bar.pdf", true},
		{"/foo/bar.zip", true},
		{"/foo/bar.mp3", true},
		{"/foo/bar.sqlite", true},
		{"/foo/bar.go", false},
		{"/foo/bar.py", false},
		{"/foo/bar.js", false},
		{"/foo/bar.md", false},
		{"/foo/bar.txt", false},
		{"/foo/bar.json", false},
		{"/foo/bar", false},
		{"", false},
	}

	for _, tt := range tests {
		got := detectBinaryByExtension(tt.path)
		if got != tt.binary {
			t.Errorf("detectBinaryByExtension(%q) = %v, want %v", tt.path, got, tt.binary)
		}
	}
}

func TestDetectBinaryByContent(t *testing.T) {
	t.Run("text content", func(t *testing.T) {
		content := []byte("Hello, world!\nThis is a text file.\n")
		if detectBinaryByContent(content) {
			t.Error("expected text content to not be detected as binary")
		}
	})

	t.Run("empty content", func(t *testing.T) {
		if detectBinaryByContent(nil) {
			t.Error("expected empty content to not be detected as binary")
		}
	})

	t.Run("binary content with NULL bytes", func(t *testing.T) {
		content := []byte("hello\x00world")
		if !detectBinaryByContent(content) {
			t.Error("expected content with NULL bytes to be detected as binary")
		}
	})

	t.Run("NULL at start", func(t *testing.T) {
		content := make([]byte, 100)
		content[0] = 0
		if !detectBinaryByContent(content) {
			t.Error("expected content with NULL at start to be detected as binary")
		}
	})

	t.Run("NULL beyond 8192 bytes", func(t *testing.T) {
		content := make([]byte, 10000)
		for i := range content {
			content[i] = 'a'
		}
		content[9000] = 0 // beyond the 8192 check window
		if detectBinaryByContent(content) {
			t.Error("expected NULL beyond 8192 bytes to not be detected")
		}
	})
}
