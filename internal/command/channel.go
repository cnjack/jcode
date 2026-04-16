package command

import (
	"time"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tui"
)

// handleChannelAction processes channel actions from the TUI.
func (s *interactiveState) handleChannelAction(action tui.ChannelAction) {
	switch action.ChannelID {
	case "wechat":
		s.handleWeChatAction(action.Action)
	default:
		s.p.Send(tui.ChannelStateMsg{
			ChannelID: action.ChannelID,
			State:     "none",
			Message:   "Unknown channel: " + action.ChannelID,
		})
	}
}

func (s *interactiveState) handleWeChatAction(action string) {
	switch action {
	case "login":
		s.p.Send(tui.ChannelStateMsg{
			ChannelID: "wechat",
			State:     s.wechatClient.State().String(),
			Message:   "Fetching login QR code...",
		})

		session, err := s.wechatClient.Login()
		if err != nil {
			s.p.Send(tui.ChannelStateMsg{
				ChannelID: "wechat",
				State:     "none",
				Message:   "Login failed: " + err.Error(),
			})
			return
		}

		// Send QR code URL to TUI
		s.p.Send(tui.ChannelQRCodeMsg{
			ChannelID:     "wechat",
			QRCodeURL:     session.QRCodeURL,
			QRCodeContent: session.QRCodeContent,
			Message:       "Scan the QR code with WeChat to login",
		})

		// Wait for scan in background
		go func() {
			if err := session.WaitFunc(); err != nil {
				s.p.Send(tui.ChannelStateMsg{
					ChannelID: "wechat",
					State:     "none",
					Message:   "Login failed: " + err.Error(),
				})
				return
			}

			s.p.Send(tui.ChannelStateMsg{
				ChannelID: "wechat",
				State:     s.wechatClient.State().String(),
				Message:   "WeChat login successful",
			})

			// Auto-enable after login
			s.handleWeChatAction("enable")
		}()

	case "logout":
		if err := s.wechatClient.Logout(); err != nil {
			s.p.Send(tui.ChannelStateMsg{
				ChannelID: "wechat",
				State:     s.wechatClient.State().String(),
				Message:   "Logout failed: " + err.Error(),
			})
			return
		}
		s.p.Send(tui.ChannelStateMsg{
			ChannelID: "wechat",
			State:     "none",
			Message:   "WeChat logged out",
		})

	case "enable":
		// Set up inbound message handler before enabling
		s.wechatClient.SetOnMessage(func(from, text string) {
			// Notify user if agent is busy
			if s.agentRunning.Load() {
				_ = s.wechatClient.SendText(channel.BusyMessage())
			}
			s.p.Send(tui.ChannelInboundMsg{
				ChannelID: "wechat",
				From:      from,
				Text:      text,
			})
		})

		if err := s.wechatClient.Enable(); err != nil {
			s.p.Send(tui.ChannelStateMsg{
				ChannelID: "wechat",
				State:     s.wechatClient.State().String(),
				Message:   "Enable failed: " + err.Error(),
			})
			return
		}
		s.p.Send(tui.ChannelStateMsg{
			ChannelID: "wechat",
			State:     channel.StateEnabled.String(),
			Message:   "WeChat channel enabled",
		})

		// Send welcome message to the newly connected user (async, may wait for first poll)
		go func() {
			if err := s.wechatClient.SendText(channel.WelcomeMessage(time.Now())); err != nil {
				config.Logger().Printf("[wechat] failed to send welcome: %v", err)
			}
		}()

	case "disable":
		if err := s.wechatClient.Disable(); err != nil {
			s.p.Send(tui.ChannelStateMsg{
				ChannelID: "wechat",
				State:     s.wechatClient.State().String(),
				Message:   "Disable failed: " + err.Error(),
			})
			return
		}
		s.p.Send(tui.ChannelStateMsg{
			ChannelID: "wechat",
			State:     channel.StateDisabled.String(),
			Message:   "WeChat channel disabled",
		})
	}
}
