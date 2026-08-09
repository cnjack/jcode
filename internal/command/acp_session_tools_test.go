package command

import (
	"context"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestACPDoesNotExposeOrAcceptSessionToolOptions(t *testing.T) {
	if options := acpSessionConfigOptions(&acpSession{}); len(options) != 0 {
		t.Fatalf("session tool options = %#v", options)
	}
	_, err := (&acpAgent{}).setSessionToolConfigOption(context.Background(), &acp.SetSessionConfigOptionBoolean{
		SessionId: "sess-test", ConfigId: "web_search", Value: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown session config option") {
		t.Fatalf("removed web_search option error = %v", err)
	}
}
