package channel

import "time"

// ChannelNotifier wraps a Channel as a Notifier, sending rich-text
// lifecycle notifications (working, idle, done) via the channel's SendText.
//
// Approval events are skipped because channels like WeChat handle approvals
// separately with a delay mechanism in NotifyingHandler.
type ChannelNotifier struct {
	ch Channel
}

// NewChannelNotifier wraps an existing Channel as a Notifier.
func NewChannelNotifier(ch Channel) *ChannelNotifier {
	return &ChannelNotifier{ch: ch}
}

func (n *ChannelNotifier) Name() string { return n.ch.ID() }

func (n *ChannelNotifier) Available() bool {
	return n.ch.State() == StateEnabled
}

func (n *ChannelNotifier) Notify(event NotifyEvent) {
	if n.ch.State() != StateEnabled {
		return
	}

	now := time.Now()
	var text string

	switch event.Type {
	case EventWorking:
		text = RichWorking(now)
	case EventIdle:
		text = RichIdle(now)
	case EventDone:
		// DoneMessage is already sent via SetDoneNotifier with full summary,
		// so we skip it here to avoid duplicate notifications.
		return
	case EventApproval:
		// Approval is handled separately via SetApprovalNotifier with delay.
		return
	default:
		return
	}

	// Best-effort, ignore errors.
	_ = n.ch.SendText(text)
}

func (n *ChannelNotifier) Close() {
	// Channel lifecycle is managed externally (login/logout/enable/disable).
}
