package web

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	channelpkg "github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/feature"
)

func (s *Server) handleChannelStatus(w http.ResponseWriter, r *http.Request) {
	if s.wechatClient == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"state":     "none",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"channel":   "wechat",
		"state":     s.wechatClient.State().String(),
	})
}

func (s *Server) handleChannelLogin(w http.ResponseWriter, r *http.Request) {
	if s.wechatClient == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel not available"})
		return
	}
	session, err := s.wechatClient.Login()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Return QR code content (URL to encode) — frontend renders the QR code
	// to avoid CORS issues with the official QR image URL.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "pending",
		"qr_content": session.QRCodeContent,
	})

	// Wait for scan in background and auto-enable
	go func() {
		if err := session.WaitFunc(); err != nil {
			config.Logger().Printf("[wechat] web login scan failed: %v", err)
			return
		}
		config.Logger().Printf("[wechat] web login scan successful, auto-enabling")
		if err := s.wechatClient.Enable(); err != nil {
			config.Logger().Printf("[wechat] web auto-enable after login failed: %v", err)
			return
		}
		// Send welcome and login reminder messages
		go func() {
			if err := s.wechatClient.SendText(channelpkg.WelcomeMessage(time.Now())); err != nil {
				config.Logger().Printf("[wechat] failed to send welcome: %v", err)
			}
			if err := s.wechatClient.SendText(channelpkg.LoginReminderMessage()); err != nil {
				config.Logger().Printf("[wechat] failed to send login reminder: %v", err)
			}
		}()
	}()
}

func (s *Server) handleChannelLogout(w http.ResponseWriter, r *http.Request) {
	if s.wechatClient == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel not available"})
		return
	}
	if s.wechatClient.State() == channelpkg.StateEnabled {
		_ = s.wechatClient.SendText(channelpkg.GoodbyeMessage(time.Now()))
	}
	if err := s.wechatClient.Logout(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "state": "none"})
}

func (s *Server) handleChannelEnable(w http.ResponseWriter, r *http.Request) {
	if s.wechatClient == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel not available"})
		return
	}
	if err := s.wechatClient.Enable(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "state": "enabled"})
}

func (s *Server) handleChannelDisable(w http.ResponseWriter, r *http.Request) {
	if s.wechatClient == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel not available"})
		return
	}
	if err := s.wechatClient.Disable(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "state": "disabled"})
}

// handleChannelBLEStatus reports whether the Bluetooth (BLE) status channel is
// enabled in config. The actual BLE notifier is only wired at startup (and only
// on desktop builds with CoreBluetooth), so this reflects the persisted
// preference, which takes effect on the next launch.
func (s *Server) handleChannelBLEStatus(w http.ResponseWriter, r *http.Request) {
	enabled := false
	s.cfgMu.Lock()
	if s.cfg != nil && s.cfg.Channel != nil {
		enabled = s.cfg.Channel.BLEEnabled
	}
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "available": feature.BLE})
}

// handleSetChannelBLE persists the Bluetooth (BLE) status-channel preference.
// Like the proxy/cert settings, it takes effect after an app restart (the BLE
// notifier is created once at startup when channel.ble_enabled is true).
func (s *Server) handleSetChannelBLE(w http.ResponseWriter, r *http.Request) {
	if !feature.BLE {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bluetooth channel is not available in this build"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	s.cfgMu.Lock()
	if s.cfg == nil {
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := s.cfg.Channel
	updated := &config.ChannelConfig{}
	if previous != nil {
		*updated = *previous
	}
	updated.BLEEnabled = req.Enabled
	s.cfg.Channel = updated
	if err := config.SaveConfig(s.cfg); err != nil {
		s.cfg.Channel = previous
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.cfgMu.Unlock()

	// Apply live: start/stop the BLE helper now so the toggle takes effect
	// without an app restart (and the macOS Bluetooth prompt / device connect
	// happens right when the user turns it on).
	if s.bleController != nil {
		if req.Enabled {
			s.bleController.Enable()
		} else {
			s.bleController.Disable()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

// Ensure writeJSON is used (defined in server.go).
var _ = json.Marshal
