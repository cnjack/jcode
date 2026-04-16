// Package weixin is the channel.Channel implementation for WeChat.
// It wraps the iLink API (internal/pkg/weixin/api.go) with credential
// persistence, poll loop lifecycle, and the jcode channel interface.
package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
)

const (
	maxConsecutiveFailures = 3
	backoffDelay           = 30 * time.Second
	retryDelay             = 2 * time.Second
	sendRetryDeadline      = 45 * time.Second
	sendRetryInterval      = 3 * time.Second
	loginTimeout           = 8 * time.Minute
)

// Credentials holds the persisted login state.
type Credentials struct {
	Token   string `json:"token"`
	BaseURL string `json:"base_url"`
	UserID  string `json:"user_id"`
	SyncBuf string `json:"sync_buf,omitempty"`
	SavedAt string `json:"saved_at"`
}

// Client implements channel.Channel for WeChat via the iLink Bot API.
type Client struct {
	mu        sync.Mutex
	state     channel.State
	creds     *Credentials
	cancel    context.CancelFunc
	onMessage func(from, text string)
}

// NewClient creates a new WeChat channel client, loading any saved credentials.
func NewClient() *Client {
	c := &Client{state: channel.StateNone}
	if creds, err := loadCredentials(); err == nil && creds.Token != "" {
		c.creds = creds
		c.state = channel.StateDisabled
	}
	return c
}

func (c *Client) ID() string { return "wechat" }

func (c *Client) State() channel.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// SetOnMessage registers a callback for inbound messages.
func (c *Client) SetOnMessage(fn func(from, text string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onMessage = fn
}

// Login initiates a QR code login flow and returns a LoginSession.
func (c *Client) Login() (*channel.LoginSession, error) {
	qrContent, sessionKey, err := FetchQRCode(DefaultBaseURL)
	if err != nil {
		return nil, fmt.Errorf("fetch QR code: %w", err)
	}

	sess := &channel.LoginSession{
		QRCodeURL:     qrContent,
		QRCodeContent: qrContent,
		SessionKey:    sessionKey,
	}
	sess.WaitFunc = func() error {
		result := PollLoginUntilDone(DefaultBaseURL, sessionKey, loginTimeout)
		if !result.Connected {
			return fmt.Errorf("login failed: %s", result.Message)
		}
		creds := &Credentials{
			Token:   result.Token,
			BaseURL: result.BaseURL,
			UserID:  result.UserID,
			SavedAt: time.Now().Format(time.RFC3339),
		}
		if err := saveCredentials(creds); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		c.mu.Lock()
		c.creds = creds
		c.state = channel.StateDisabled
		c.mu.Unlock()
		return nil
	}
	return sess, nil
}

// Logout clears credentials and stops polling.
func (c *Client) Logout() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.creds = nil
	c.state = channel.StateNone
	return deleteCredentials()
}

// Enable starts the inbound message poll loop.
func (c *Client) Enable() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creds == nil || c.creds.Token == "" {
		return fmt.Errorf("not logged in, please login first")
	}
	if c.state == channel.StateEnabled {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.state = channel.StateEnabled
	go c.pollLoop(ctx)
	return nil
}

// Disable stops polling but keeps credentials.
func (c *Client) Disable() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.creds != nil {
		c.state = channel.StateDisabled
	} else {
		c.state = channel.StateNone
	}
	return nil
}

// SendText sends a text message to the connected user.
// Retries for up to 45 s if the session is not yet activated (ret=-2).
func (c *Client) SendText(text string) error {
	c.mu.Lock()
	creds := c.creds
	state := c.state
	c.mu.Unlock()

	if state != channel.StateEnabled {
		return fmt.Errorf("channel not enabled")
	}
	if creds == nil {
		return fmt.Errorf("not logged in")
	}

	baseURL := creds.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	deadline := time.Now().Add(sendRetryDeadline)
	for {
		err := SendText(baseURL, creds.Token, creds.UserID, text, "")
		if err == nil {
			config.Logger().Printf("[weixin] sent %d bytes to %s", len(text), creds.UserID)
			return nil
		}
		if IsSessionNotReady(err) && time.Now().Before(deadline) {
			time.Sleep(sendRetryInterval)
			continue
		}
		return err
	}
}

// pollLoop is the long-poll background goroutine.
func (c *Client) pollLoop(ctx context.Context) {
	consecutiveFailures := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		creds := c.creds
		var syncBuf string
		if creds != nil {
			syncBuf = creds.SyncBuf
		}
		c.mu.Unlock()

		if creds == nil {
			return
		}

		baseURL := creds.BaseURL
		if baseURL == "" {
			baseURL = DefaultBaseURL
		}

		resp, err := GetUpdates(ctx, baseURL, creds.Token, syncBuf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if IsSessionExpired(err) {
				config.Logger().Printf("[weixin] session expired, disabling")
				_ = c.Disable()
				return
			}
			consecutiveFailures++
			wait := retryDelay
			if consecutiveFailures >= maxConsecutiveFailures {
				consecutiveFailures = 0
				wait = backoffDelay
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			continue
		}

		consecutiveFailures = 0

		if resp.GetUpdatesBuf != "" {
			c.mu.Lock()
			if c.creds != nil {
				c.creds.SyncBuf = resp.GetUpdatesBuf
				_ = saveCredentials(c.creds)
			}
			c.mu.Unlock()
		}

		for _, msg := range resp.Msgs {
			text := msg.TextBody()
			if text == "" {
				continue
			}
			c.mu.Lock()
			handler := c.onMessage
			c.mu.Unlock()
			if handler != nil {
				handler(msg.FromUserID, text)
			}
		}
	}
}

// --- Credential persistence ---

func credentialsPath() string {
	return filepath.Join(config.ConfigDir(), "channel", "wechat.json")
}

func legacyCredentialsPath() string {
	return filepath.Join(config.ConfigDir(), "wechat.json")
}

func legacySyncBufPath() string {
	return filepath.Join(config.ConfigDir(), "wechat_sync.buf")
}

func loadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		// Try legacy paths and migrate
		data, err = os.ReadFile(legacyCredentialsPath())
		if err != nil {
			return nil, err
		}
		var creds Credentials
		if err := json.Unmarshal(data, &creds); err != nil {
			return nil, err
		}
		if buf, bufErr := os.ReadFile(legacySyncBufPath()); bufErr == nil {
			creds.SyncBuf = string(buf)
		}
		_ = saveCredentials(&creds)
		_ = os.Remove(legacyCredentialsPath())
		_ = os.Remove(legacySyncBufPath())
		return &creds, nil
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func saveCredentials(creds *Credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(config.ConfigDir(), "channel")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(credentialsPath(), data, 0o600)
}

func deleteCredentials() error {
	if err := os.Remove(credentialsPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(legacyCredentialsPath())
	_ = os.Remove(legacySyncBufPath())
	return nil
}
