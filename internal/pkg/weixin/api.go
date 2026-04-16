// Package weixin implements the WeChat iLink Bot API client.
// It provides a pure HTTP SDK for QR login, long-poll message receiving,
// and text sending. No jcode-specific dependencies.
package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL     = "https://ilinkai.weixin.qq.com"
	defaultBotType     = "3"
	sendTimeoutMs      = 15000
	pollHTTPTimeoutSec = 40
)

// --- Wire types ---

type qrCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type qrStatusResponse struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	ILinkBotID   string `json:"ilink_bot_id"`
	BaseURL      string `json:"baseurl"`
	ILinkUserID  string `json:"ilink_user_id"`
	RedirectHost string `json:"redirect_host"`
}

type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
}

type getUpdatesRequest struct {
	GetUpdatesBuf string   `json:"get_updates_buf"`
	BaseInfo      baseInfo `json:"base_info"`
}

// GetUpdatesResponse is the parsed response from /ilink/bot/getupdates.
type GetUpdatesResponse struct {
	Ret           int       `json:"ret"`
	ErrCode       int       `json:"errcode"`
	ErrMsg        string    `json:"errmsg"`
	Msgs          []Message `json:"msgs"`
	GetUpdatesBuf string    `json:"get_updates_buf"`
}

// Message is a single inbound WeChat message.
type Message struct {
	Seq          int    `json:"seq"`
	MessageID    int    `json:"message_id"`
	FromUserID   string `json:"from_user_id"`
	ToUserID     string `json:"to_user_id"`
	ClientID     string `json:"client_id"`
	CreateTimeMs int64  `json:"create_time_ms"`
	MessageType  int    `json:"message_type"`
	MessageState int    `json:"message_state"`
	ItemList     []Item `json:"item_list"`
	ContextToken string `json:"context_token"`
}

// TextBody extracts the text content from the message, or returns empty string.
func (m Message) TextBody() string {
	for _, item := range m.ItemList {
		if item.Type == ItemTypeText && item.TextItem != nil {
			return item.TextItem.Text
		}
	}
	return ""
}

// Item is a single content item within a message.
type Item struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

// TextItem holds the text payload.
type TextItem struct {
	Text string `json:"text"`
}

// Item type constants.
const (
	ItemTypeText  = 1
	ItemTypeImage = 2
	ItemTypeVoice = 3
	ItemTypeFile  = 4
	ItemTypeVideo = 5
)

type sendMessageRequest struct {
	Msg      sendMsg  `json:"msg"`
	BaseInfo baseInfo `json:"base_info"`
}

type sendMsg struct {
	FromUserID   string `json:"from_user_id"`
	ToUserID     string `json:"to_user_id"`
	ClientID     string `json:"client_id"`
	MessageType  int    `json:"message_type"`
	MessageState int    `json:"message_state"`
	ItemList     []Item `json:"item_list"`
	ContextToken string `json:"context_token,omitempty"`
}

// LoginResult is the outcome of a QR login flow.
type LoginResult struct {
	Connected bool
	Token     string
	BaseURL   string
	UserID    string
	Message   string
}

// --- API functions ---

// FetchQRCode fetches a login QR code from the iLink service.
// Returns QRCodeImgContent (the URL to encode as QR) and QRCode (the session polling key).
func FetchQRCode(baseURL string) (qrContent, sessionKey string, err error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u := fmt.Sprintf("%s/ilink/bot/get_bot_qrcode?bot_type=%s", baseURL, url.QueryEscape(defaultBotType))
	resp, err := http.Get(u) //nolint:gosec
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var r qrCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return r.QRCodeImgContent, r.QRCode, nil
}

// PollLoginUntilDone polls the QR scan status until confirmed, expired, or timeout.
// It handles IDC redirects (scaned_but_redirect) transparently.
func PollLoginUntilDone(baseURL, sessionKey string, timeout time.Duration) *LoginResult {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := &http.Client{Timeout: pollHTTPTimeoutSec * time.Second}
	currentBase := baseURL
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		u := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", currentBase, url.QueryEscape(sessionKey))
		resp, err := client.Get(u)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var r qrStatusResponse
		_ = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close() //nolint:errcheck

		switch r.Status {
		case "wait", "scaned":
			// continue
		case "confirmed":
			resultBase := DefaultBaseURL
			if r.BaseURL != "" {
				resultBase = r.BaseURL
			}
			return &LoginResult{
				Connected: true,
				Token:     r.BotToken,
				BaseURL:   resultBase,
				UserID:    r.ILinkUserID,
				Message:   "login successful",
			}
		case "expired":
			return &LoginResult{Connected: false, Message: "QR code expired"}
		case "scaned_but_redirect":
			if r.RedirectHost != "" {
				currentBase = r.RedirectHost
			}
		}
	}
	return &LoginResult{Connected: false, Message: "login timed out"}
}

// GetUpdates performs one long-poll for inbound messages.
// The context controls the HTTP request lifetime; cancel it to stop the poll.
func GetUpdates(ctx context.Context, baseURL, token, syncBuf string) (*GetUpdatesResponse, error) {
	body, err := json.Marshal(getUpdatesRequest{
		GetUpdatesBuf: syncBuf,
		BaseInfo:      baseInfo{ChannelVersion: "jcode/1.0.0"},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/ilink/bot/getupdates", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setAuthHeaders(req, token)

	hc := &http.Client{Timeout: pollHTTPTimeoutSec * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("session expired (401)")
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var r GetUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Ret != 0 || r.ErrCode != 0 {
		return nil, fmt.Errorf("getupdates error: ret=%d errcode=%d errmsg=%s", r.Ret, r.ErrCode, r.ErrMsg)
	}
	return &r, nil
}

// SendText sends a text message to toUserID.
// contextToken should be echoed from the inbound message when replying; pass "" for proactive sends.
func SendText(baseURL, token, toUserID, text, contextToken string) error {
	clientID := fmt.Sprintf("jcode-%d", time.Now().UnixNano())
	body, err := json.Marshal(sendMessageRequest{
		Msg: sendMsg{
			FromUserID:   "",
			ToUserID:     toUserID,
			ClientID:     clientID,
			MessageType:  2, // BOT
			MessageState: 2, // FINISH
			ItemList: []Item{
				{Type: ItemTypeText, TextItem: &TextItem{Text: text}},
			},
			ContextToken: contextToken,
		},
		BaseInfo: baseInfo{ChannelVersion: "jcode/1.0.0"},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/ilink/bot/sendmessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	setAuthHeaders(req, token)

	hc := &http.Client{Timeout: time.Duration(sendTimeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sendmessage HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && (result.Ret != 0 || result.ErrCode != 0) {
		return fmt.Errorf("sendmessage API error: ret=%d errcode=%d errmsg=%s", result.Ret, result.ErrCode, result.ErrMsg)
	}
	return nil
}

func setAuthHeaders(req *http.Request, token string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("AuthorizationType", "ilink_bot_token")
}

// IsSessionNotReady returns true if the error indicates the session has not
// been activated yet (getupdates has not completed at least once).
func IsSessionNotReady(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ret=-2")
}

// IsSessionExpired returns true if the error indicates the bot token is invalid.
func IsSessionExpired(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "session expired") ||
		strings.Contains(err.Error(), "errcode=-14") ||
		strings.Contains(err.Error(), "ret=-14")
}
