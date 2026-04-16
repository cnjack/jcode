//go:build ignore

// WeChat iLink Bot API POC
// Usage: go run script/poc/weixin_poc.go
//
// Flow:
//   1. Fetch QR code and display in terminal
//   2. User scans with WeChat
//   3. Poll until login confirmed → obtain bot_token
//   4. Start receiving messages via getupdates long-poll
//   5. Echo every inbound text message back to sender

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
)

const (
	defaultBaseURL = "https://ilinkai.weixin.qq.com"
	botType        = "3"
)

// --- API Types ---

type qrCodeResp struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type qrStatusResp struct {
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

type getUpdatesReq struct {
	GetUpdatesBuf string   `json:"get_updates_buf"`
	BaseInfo      baseInfo `json:"base_info"`
}

type messageItem struct {
	Type     int       `json:"type"`
	TextItem *textItem `json:"text_item,omitempty"`
}

type textItem struct {
	Text string `json:"text"`
}

type weixinMessage struct {
	Seq          int           `json:"seq"`
	MessageID    int           `json:"message_id"`
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	ClientID     string        `json:"client_id"`
	CreateTimeMs int64         `json:"create_time_ms"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state"`
	ItemList     []messageItem `json:"item_list"`
	ContextToken string        `json:"context_token"`
}

type getUpdatesResp struct {
	Ret           int             `json:"ret"`
	ErrCode       int             `json:"errcode"`
	ErrMsg        string          `json:"errmsg"`
	Msgs          []weixinMessage `json:"msgs"`
	GetUpdatesBuf string          `json:"get_updates_buf"`
}

type sendMsgReq struct {
	Msg      sendMsg  `json:"msg"`
	BaseInfo baseInfo `json:"base_info"`
}

type sendMsg struct {
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	ClientID     string        `json:"client_id"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state"`
	ItemList     []messageItem `json:"item_list"`
	ContextToken string        `json:"context_token,omitempty"`
}

// --- Session state ---

type session struct {
	BaseURL string
	Token   string
	UserID  string
	SyncBuf string
}

// --- Step 1: Fetch QR code ---

func fetchQR(baseURL string) (*qrCodeResp, error) {
	u := fmt.Sprintf("%s/ilink/bot/get_bot_qrcode?bot_type=%s", baseURL, url.QueryEscape(botType))
	resp, err := http.Get(u) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r qrCodeResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// --- Step 2: Poll QR scan status until confirmed ---

func pollLogin(baseURL, qrcode string) (*session, error) {
	client := &http.Client{Timeout: 40 * time.Second}
	currentBase := baseURL
	deadline := time.Now().Add(8 * time.Minute)

	for time.Now().Before(deadline) {
		u := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", currentBase, url.QueryEscape(qrcode))
		resp, err := client.Get(u)
		if err != nil {
			log.Printf("[warn] poll error: %v — retrying", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var r qrStatusResp
		_ = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()

		switch r.Status {
		case "wait":
			// continue polling
		case "scaned":
			fmt.Println("[*] QR scanned — waiting for confirmation...")
		case "confirmed":
			s := &session{
				BaseURL: defaultBaseURL,
				Token:   r.BotToken,
				UserID:  r.ILinkUserID,
			}
			if r.BaseURL != "" {
				s.BaseURL = r.BaseURL
			}
			return s, nil
		case "expired":
			return nil, fmt.Errorf("QR code expired")
		case "scaned_but_redirect":
			if r.RedirectHost != "" {
				currentBase = r.RedirectHost
				log.Printf("[*] Redirecting to %s", currentBase)
			}
		}
	}
	return nil, fmt.Errorf("login timed out")
}

// --- Step 3: Send a text message ---

func sendText(s *session, toUserID, text, contextToken string) error {
	clientID := fmt.Sprintf("poc-%d", time.Now().UnixNano())
	body := sendMsgReq{
		Msg: sendMsg{
			FromUserID:   "",
			ToUserID:     toUserID,
			ClientID:     clientID,
			MessageType:  2, // BOT
			MessageState: 2, // FINISH
			ItemList: []messageItem{
				{Type: 1, TextItem: &textItem{Text: text}},
			},
			ContextToken: contextToken,
		},
		BaseInfo: baseInfo{ChannelVersion: "poc/1.0.0"},
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/ilink/bot/sendmessage", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("AuthorizationType", "ilink_bot_token")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && (result.Ret != 0 || result.ErrCode != 0) {
		return fmt.Errorf("API error ret=%d errcode=%d errmsg=%s", result.Ret, result.ErrCode, result.ErrMsg)
	}
	return nil
}

// --- Step 4: Long-poll loop ---

func pollMessages(s *session) {
	log.Printf("[*] Listening for messages (echo bot)...")

	for {
		body := getUpdatesReq{
			GetUpdatesBuf: s.SyncBuf,
			BaseInfo:      baseInfo{ChannelVersion: "poc/1.0.0"},
		}
		data, _ := json.Marshal(body)

		req, err := http.NewRequest(http.MethodPost, s.BaseURL+"/ilink/bot/getupdates", bytes.NewReader(data))
		if err != nil {
			log.Printf("[error] build request: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("AuthorizationType", "ilink_bot_token")

		client := &http.Client{Timeout: 40 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[warn] getupdates error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		var r getUpdatesResp
		_ = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()

		if r.Ret != 0 || r.ErrCode != 0 {
			log.Printf("[warn] getupdates API error ret=%d errcode=%d errmsg=%s", r.Ret, r.ErrCode, r.ErrMsg)
			time.Sleep(2 * time.Second)
			continue
		}

		if r.GetUpdatesBuf != "" {
			s.SyncBuf = r.GetUpdatesBuf
		}

		for _, msg := range r.Msgs {
			text := extractText(msg)
			if text == "" {
				continue
			}
			log.Printf("[msg] from=%s text=%q", msg.FromUserID, text)

			// Echo the message back
			reply := fmt.Sprintf("Echo: %s", text)
			if err := sendText(s, msg.FromUserID, reply, msg.ContextToken); err != nil {
				log.Printf("[error] send failed: %v", err)
			} else {
				log.Printf("[sent] echo to %s", msg.FromUserID)
			}
		}
	}
}

func extractText(msg weixinMessage) string {
	for _, item := range msg.ItemList {
		if item.Type == 1 && item.TextItem != nil {
			return item.TextItem.Text
		}
	}
	return ""
}

// --- Main ---

func main() {
	fmt.Println("=== WeChat iLink Bot POC ===")

	// Step 1: Fetch QR
	fmt.Println("\n[1] Fetching QR code...")
	qr, err := fetchQR(defaultBaseURL)
	if err != nil {
		log.Fatalf("fetch QR: %v", err)
	}

	// Render QR in terminal
	fmt.Println("\nScan with WeChat:")
	qrterminal.GenerateHalfBlock(qr.QRCodeImgContent, qrterminal.L, os.Stdout)
	fmt.Printf("\n    URL: %s\n\n", qr.QRCodeImgContent)

	// Step 2: Wait for login
	fmt.Println("[2] Waiting for scan...")
	s, err := pollLogin(defaultBaseURL, qr.QRCode)
	if err != nil {
		log.Fatalf("login: %v", err)
	}
	fmt.Printf("\n[+] Logged in! user_id=%s base_url=%s\n", s.UserID, s.BaseURL)

	// Step 3: Send a greeting (may fail with ret=-2 until first getupdates completes — that's fine)
	fmt.Println("\n[3] Sending greeting (first getupdates activates session, greeting may retry)...")
	go func() {
		// Retry a few times for session activation
		for i := range 10 {
			err := sendText(s, s.UserID, "Hello from jcode POC! Send me a message and I'll echo it back.", "")
			if err == nil {
				log.Printf("[+] Greeting sent")
				return
			}
			if strings.Contains(err.Error(), "ret=-2") {
				log.Printf("[wait] session not ready yet (attempt %d), retrying in 3s...", i+1)
				time.Sleep(3 * time.Second)
				continue
			}
			log.Printf("[warn] greeting failed: %v", err)
			return
		}
	}()

	// Step 4: Long-poll loop (blocks)
	fmt.Println("\n[4] Listening for messages (Ctrl+C to quit)...")
	pollMessages(s)
}
