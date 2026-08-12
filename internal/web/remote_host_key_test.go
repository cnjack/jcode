package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cnjack/jcode/internal/remote"
)

func TestWriteSSHHostKeyErrorContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := fmt.Errorf("connect: %w", &remote.SSHHostKeyError{
		Code:           remote.SSHHostKeyChanged,
		Host:           "[example.test]:2222",
		Fingerprint:    "SHA256:new",
		KeyType:        "ssh-ed25519",
		OldFingerprint: "SHA256:old",
	})
	if !writeSSHHostKeyError(recorder, err) {
		t.Fatal("structured host-key error was not handled")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"code":            remote.SSHHostKeyChanged,
		"host":            "[example.test]:2222",
		"fingerprint":     "SHA256:new",
		"key_type":        "ssh-ed25519",
		"old_fingerprint": "SHA256:old",
	} {
		if got := body[key]; got != want {
			t.Fatalf("response %s = %#v, want %q", key, got, want)
		}
	}
}

func TestWriteSSHHostKeyErrorIgnoresOrdinaryFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	if writeSSHHostKeyError(recorder, fmt.Errorf("authentication failed")) {
		t.Fatal("ordinary SSH error was classified as a host-key error")
	}
}
