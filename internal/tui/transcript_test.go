package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestTranscriptRendersFullOutput asserts the overlay renders tool output
// untruncated — including middle lines the main timeline hides — plus the
// duration metadata.
func TestTranscriptRendersFullOutput(t *testing.T) {
	m := newToolTestModel()

	m.Update(ToolCallMsg{Name: "execute", Title: "Shell", Subtitle: "run tests", ToolCallID: "t1"})
	var out strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&out, "row%02d\n", i)
	}
	m.Update(ToolResultMsg{Name: "execute", Output: out.String(), ToolCallID: "t1", Duration: 3 * time.Second})

	tr := m.renderTranscript(100)
	for _, want := range []string{"row01", "row25", "row50", "3.0s", "Shell"} {
		if !strings.Contains(tr, want) {
			t.Errorf("transcript missing %q", want)
		}
	}
	if strings.Contains(tr, transcriptHint) {
		t.Errorf("transcript itself must not carry truncation markers")
	}
}

// TestTranscriptRendersErrors asserts failed tools show their full error text.
func TestTranscriptRendersErrors(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{Name: "execute", Title: "Shell", ToolCallID: "e1"})
	m.Update(ToolResultMsg{Name: "execute", ToolCallID: "e1",
		Err: errors.New("exit status 1: everything is on fire"), Duration: time.Second})

	tr := m.renderTranscript(100)
	if !strings.Contains(tr, "everything is on fire") || !strings.Contains(tr, "Error:") {
		t.Errorf("transcript missing full error text:\n%s", tr)
	}
}

// TestTranscriptOpenCloseKeys drives the overlay lifecycle through Update:
// open directly, close via esc, reopen, close via ctrl+t.
func TestTranscriptOpenCloseKeys(t *testing.T) {
	m := newToolTestModel()
	m.textarea = newTextarea()
	m.width, m.height = 100, 30

	m.openTranscript()
	if !m.showingTranscript {
		t.Fatal("openTranscript did not open the overlay")
	}
	if m.inputActive() {
		t.Fatal("input must be inactive while the transcript is open")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showingTranscript {
		t.Fatal("esc did not close the overlay")
	}

	m.openTranscript()
	m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.showingTranscript {
		t.Fatal("ctrl+t did not close the overlay")
	}
}

// TestTranscriptOpenGuards pins that the overlay refuses to open before the
// first WindowSizeMsg (no dimensions yet).
func TestTranscriptOpenGuards(t *testing.T) {
	m := newToolTestModel()
	m.openTranscript()
	if m.showingTranscript {
		t.Fatal("overlay opened without terminal dimensions")
	}
}

// TestTranscriptViewHasHints asserts the overlay chrome: header, key hints,
// scroll percentage.
func TestTranscriptViewHasHints(t *testing.T) {
	m := newToolTestModel()
	m.textarea = newTextarea()
	m.width, m.height = 100, 30
	m.lines = append(m.lines, textLine("hello world"))
	m.openTranscript()

	v := m.transcriptView()
	for _, want := range []string{"Transcript", "esc close", "%", "hello world"} {
		if !strings.Contains(v, want) {
			t.Errorf("transcript view missing %q", want)
		}
	}
}
