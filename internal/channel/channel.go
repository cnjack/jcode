// Package channel provides an abstraction for external messaging channels
// (e.g. WeChat, Telegram) that connect to the jcode agent as lightweight
// notification + remote prompt sidecars.
package channel

// State represents the lifecycle state of a channel.
type State int

const (
	// StateNone means the channel has never been configured (no credentials).
	StateNone State = iota
	// StateDisabled means the channel has credentials but push/receive is off.
	StateDisabled
	// StateEnabled means the channel is actively pushing notifications and receiving messages.
	StateEnabled
)

func (s State) String() string {
	switch s {
	case StateNone:
		return "none"
	case StateDisabled:
		return "disabled"
	case StateEnabled:
		return "enabled"
	default:
		return "unknown"
	}
}

// EventType identifies the kind of lifecycle event sent to notifiers.
type EventType int

const (
	EventIdle     EventType = iota // agent is idle, waiting for user input
	EventWorking                   // agent is actively processing
	EventApproval                  // agent is blocked, waiting for tool approval
	EventDone                      // agent finished a task
)

// NotifyEvent is the structured event passed to Notifier.Notify.
type NotifyEvent struct {
	Type EventType
	Tool string // tool name (for EventApproval)
	Err  error  // non-nil on failure (for EventDone)
}

// Notifier is a lightweight one-way notification sender.
// Unlike Channel, it requires no login/configuration flow — it just sends
// short text messages to an external device or service.
// Implementations must be safe for concurrent use.
type Notifier interface {
	// Name returns a human-readable identifier (e.g. "ble", "wechat").
	Name() string
	// Available reports whether the notifier is ready to send.
	Available() bool
	// Notify pushes a lifecycle event. Implementations format the event
	// for their own display (short text for BLE, rich text for WeChat, etc.).
	// Must be best-effort and never block the caller for long.
	Notify(event NotifyEvent)
	// Close releases resources. Safe to call multiple times.
	Close()
}

// Channel is the interface that all messaging channel implementations must satisfy.
type Channel interface {
	// ID returns the channel identifier (e.g. "wechat").
	ID() string
	// State returns the current lifecycle state.
	State() State
	// Login initiates the authentication flow (e.g. QR scan).
	// Returns a login session that can be waited on.
	Login() (*LoginSession, error)
	// Logout clears credentials and stops the channel.
	Logout() error
	// Enable starts push notifications and inbound message polling.
	Enable() error
	// Disable stops push notifications and polling but keeps credentials.
	Disable() error
	// SendText sends a text message to the connected user.
	SendText(text string) error
}

// LoginSession represents an in-progress login that requires user action (e.g. QR scan).
type LoginSession struct {
	// QRCodeURL is a URL that renders a QR code image for scanning.
	QRCodeURL string
	// QRCodeContent is the raw content to encode as a QR code in terminal.
	QRCodeContent string
	// SessionKey identifies this login attempt.
	SessionKey string
	// WaitFunc blocks until login completes or times out. Returns nil on success.
	WaitFunc func() error
}
