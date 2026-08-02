//go:build !desktop

package web

import "testing"

func TestPlainWebBuildDoesNotConfigureDesktopArtifactOpener(t *testing.T) {
	srv := NewServer(&ServerConfig{})
	if srv.openArtifact != nil {
		t.Fatal("plain web builds must not expose host open/reveal actions")
	}
}
