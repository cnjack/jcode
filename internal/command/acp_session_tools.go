package command

import (
	"context"
	"fmt"

	acp "github.com/coder/acp-go-sdk"
)

// Provider-managed tools follow the active provider configuration and are not
// ACP session options. Returning an empty list keeps new clients from exposing
// the retired per-session web-search switch.
func acpSessionConfigOptions(*acpSession) []acp.SessionConfigOption {
	return nil
}

func (a *acpAgent) setSessionToolConfigOption(
	_ context.Context,
	request *acp.SetSessionConfigOptionBoolean,
) (acp.SetSessionConfigOptionResponse, error) {
	if request == nil {
		return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("unsupported session config option value")
	}
	return acp.SetSessionConfigOptionResponse{}, fmt.Errorf(
		"unknown session config option %q", request.ConfigId,
	)
}
