package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cnjack/jcode/internal/config"
)

// Mailbox provides file-based message passing between teammates.
// Each teammate has an inbox file: ~/.jcode/teams/{team}/inboxes/{sessionID}/{agent}.json
type Mailbox struct {
	baseDir string
	mu      sync.Mutex // serializes writes within this process
}

// NewMailbox creates a new mailbox scoped to a specific session.
// Path: ~/.jcode/teams/{team}/inboxes/{sessionID}/
func NewMailbox(teamName, sessionID string) *Mailbox {
	baseDir := filepath.Join(config.ConfigDir(), "teams", teamName, "inboxes", sessionID)
	_ = os.MkdirAll(baseDir, 0755)
	return &Mailbox{baseDir: baseDir}
}

func (mb *Mailbox) inboxPath(agentName string) string {
	safe := sanitizeAgentName(agentName)
	return filepath.Join(mb.baseDir, safe+".json")
}

// WriteMessage appends a message to the recipient's inbox with file-level locking.
func (mb *Mailbox) WriteMessage(recipientName string, msg TeammateMessage) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	path := mb.inboxPath(recipientName)

	// Read existing messages.
	messages, _ := readMessagesFromFile(path)

	msg.Read = false
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	messages = append(messages, msg)

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ReadAll returns all messages (read and unread) for the given agent.
func (mb *Mailbox) ReadAll(agentName string) ([]TeammateMessage, error) {
	path := mb.inboxPath(agentName)
	return readMessagesFromFile(path)
}

// ReadUnread returns only unread messages for the given agent.
func (mb *Mailbox) ReadUnread(agentName string) ([]TeammateMessage, error) {
	all, err := mb.ReadAll(agentName)
	if err != nil {
		return nil, err
	}
	var unread []TeammateMessage
	for _, m := range all {
		if !m.Read {
			unread = append(unread, m)
		}
	}
	return unread, nil
}

// ConsumeFirst reads the first unread message and marks it as read atomically.
// Returns nil if no unread messages are available.
func (mb *Mailbox) ConsumeFirst(agentName string) (*TeammateMessage, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	path := mb.inboxPath(agentName)
	messages, err := readMessagesFromFile(path)
	if err != nil {
		return nil, err
	}

	// Find first unread message.
	for i := range messages {
		if !messages[i].Read {
			messages[i].Read = true
			data, err := json.MarshalIndent(messages, "", "  ")
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				return nil, err
			}
			return &messages[i], nil
		}
	}
	return nil, nil
}

// MarkRead marks a specific message as read by index.
func (mb *Mailbox) MarkRead(agentName string, index int) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	path := mb.inboxPath(agentName)
	messages, err := readMessagesFromFile(path)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(messages) {
		return fmt.Errorf("message index %d out of range", index)
	}
	messages[index].Read = true

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Clear removes all messages from an agent's inbox.
func (mb *Mailbox) Clear(agentName string) error {
	path := mb.inboxPath(agentName)
	return os.Remove(path)
}

func readMessagesFromFile(path string) ([]TeammateMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var messages []TeammateMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("unmarshal inbox: %w", err)
	}
	return messages, nil
}

// sanitizeAgentName ensures the name is safe for use as a filename.
// Prevents path traversal attacks.
func sanitizeAgentName(name string) string {
	// Take only the base name (no directory separators).
	name = filepath.Base(name)

	// Replace any unsafe characters.
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '.' || !unicode.IsPrint(r) || r > 127 {
			return '_'
		}
		return r
	}, name)

	if name == "" || name == "_" {
		return "unknown"
	}
	return name
}

// IsShutdownRequest checks if a message text is a shutdown request.
func IsShutdownRequest(text string) bool {
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(text), &msg); err != nil {
		return false
	}
	return msg.Type == "shutdown_request"
}
