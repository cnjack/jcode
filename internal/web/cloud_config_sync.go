package web

import (
	"context"
	"net/http"
	"time"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

const cloudConfigSyncTimeout = 20 * time.Second

func (s *Server) configSyncContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), cloudConfigSyncTimeout)
}

func (s *Server) handleCloudConfigSync(w http.ResponseWriter, r *http.Request) {
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": config.CloudConfigSync(s.cfg),
			"key":     cloud.AccountSyncKeyState{State: "offline"},
		})
		return
	}
	ctx, cancel := s.configSyncContext(r)
	defer cancel()
	state, err := s.cloudSupervisor.AccountSyncKeyStatus(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.cloudSupervisor.Status().ConfigSync,
		"key":     state,
	})
}

func (s *Server) handleCloudConfigSyncNow(w http.ResponseWriter, r *http.Request) {
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cloud relay is not connected"})
		return
	}
	ctx, cancel := s.configSyncContext(r)
	defer cancel()
	if err := s.cloudSupervisor.SyncProviderConfigs(ctx); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCloudConfigSyncRequests(w http.ResponseWriter, r *http.Request) {
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"requests": []cloud.AccountSyncKeyRequest{},
			"devices":  []cloud.AccountSyncKeyRequest{},
		})
		return
	}
	ctx, cancel := s.configSyncContext(r)
	defer cancel()
	requests, err := s.cloudSupervisor.PendingAccountSyncDevices(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if requests == nil {
		requests = []cloud.AccountSyncKeyRequest{}
	}
	devices, err := s.cloudSupervisor.ApprovedAccountSyncDevices(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if devices == nil {
		devices = []cloud.AccountSyncKeyRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": requests, "devices": devices})
}

func (s *Server) handleCloudConfigSyncApprove(w http.ResponseWriter, r *http.Request) {
	s.handleCloudConfigSyncDecision(w, r, true)
}

func (s *Server) handleCloudConfigSyncDeny(w http.ResponseWriter, r *http.Request) {
	s.handleCloudConfigSyncDecision(w, r, false)
}

func (s *Server) handleCloudConfigSyncDecision(w http.ResponseWriter, r *http.Request, approve bool) {
	deviceID := r.PathValue("device_id")
	if deviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id is required"})
		return
	}
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cloud relay is not connected"})
		return
	}
	ctx, cancel := s.configSyncContext(r)
	defer cancel()
	var err error
	if approve {
		err = s.cloudSupervisor.ApproveAccountSyncDevice(ctx, deviceID)
	} else {
		err = s.cloudSupervisor.DenyAccountSyncDevice(ctx, deviceID)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCloudConfigSyncRevoke(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	if deviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id is required"})
		return
	}
	if s.cloudSupervisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cloud relay is not connected"})
		return
	}
	ctx, cancel := s.configSyncContext(r)
	defer cancel()
	if err := s.cloudSupervisor.RevokeAccountSyncDevice(ctx, deviceID); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
