package cloud

import "testing"

// TestEventDurabilityClassification pins the durable/ephemeral class of every
// WS event type the local web server emits today, plus the default for
// unknown types (durable — 宁可多存不可丢).
func TestEventDurabilityClassification(t *testing.T) {
	durable := []string{
		// complete message-level events
		"user_message", "agent_message", "tool_call", "tool_result",
		"approval_request", "ask_user_request",
		// session state changes
		"agent_start", "agent_done", "task_status",
		"todo_update", "goal_update", "session_reset",
		"mode_changed", "model_changed",
		// subagent lifecycle
		"subagent_event",
	}
	for _, typ := range durable {
		if !isDurableEvent(typ) {
			t.Errorf("event %q classified ephemeral, want durable", typ)
		}
	}

	ephemeral := []string{
		"agent_text",        // streaming assistant delta
		"token_update",      // cumulative token counters
		"subagent_progress", // intermediate progress lines
	}
	for _, typ := range ephemeral {
		if isDurableEvent(typ) {
			t.Errorf("event %q classified durable, want ephemeral", typ)
		}
	}

	if !isDurableEvent("some_future_event_type") {
		t.Error("unknown event type must default to durable")
	}
}

func TestOperationalStatusEventsAreLocalOnly(t *testing.T) {
	for _, event := range []string{"remote_connection_status", "model_retry_status"} {
		if !localOnlyEvents[event] {
			t.Fatalf("%s must be dropped before Cloud event routing", event)
		}
	}
}

func TestSeqAllocatorMonotonicFromOne(t *testing.T) {
	a := newSeqAllocator()
	for want := int64(1); want <= 3; want++ {
		if got := a.Next("s1"); got != want {
			t.Fatalf("Next(s1) = %d, want %d", got, want)
		}
	}
	// Independent per-session counters.
	if got := a.Next("s2"); got != 1 {
		t.Fatalf("Next(s2) = %d, want 1", got)
	}
}

func TestSeqAllocatorSeedResumesLastSeq(t *testing.T) {
	a := newSeqAllocator()
	a.Seed("s1", 41) // server already has up to seq 41
	if got := a.Next("s1"); got != 42 {
		t.Fatalf("Next after Seed(41) = %d, want 42", got)
	}
	// A stale (lower) seed must never move the counter backwards.
	a.Seed("s1", 10)
	if got := a.Next("s1"); got != 43 {
		t.Fatalf("Next after stale Seed = %d, want 43", got)
	}
}

func TestSeqAllocatorResyncOnConflict(t *testing.T) {
	a := newSeqAllocator()
	a.Next("s1")       // 1
	a.Next("s1")       // 2
	a.Resync("s1", 57) // server reports max_seq 57 on conflict
	if got := a.Next("s1"); got != 58 {
		t.Fatalf("Next after Resync(57) = %d, want 58", got)
	}
}
