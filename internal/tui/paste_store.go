package tui

import (
	"fmt"
	"strings"
	"sync"
)

// minLinesForReference is the minimum number of lines to trigger reference mode.
const minLinesForReference = 3

// PasteStore stores multi-line content and provides reference-based access.
// It is used to collapse long pasted content in the TUI while preserving
// the full content for the agent.
type PasteStore struct {
	mu       sync.RWMutex
	contents map[int]string // id -> content
	nextID   int
}

// NewPasteStore creates a new PasteStore.
func NewPasteStore() *PasteStore {
	return &PasteStore{
		contents: make(map[int]string),
		nextID:   1,
	}
}

// Store stores content and returns a reference ID.
// Returns 0 if content should not be stored (less than minLinesForReference lines).
func (ps *PasteStore) Store(content string) int {
	if CountLines(content) < minLinesForReference {
		return 0
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	id := ps.nextID
	ps.nextID++
	ps.contents[id] = content
	return id
}

// StoreAndFormat stores content and returns the formatted reference string.
// If content is too short to collapse, returns the original content unchanged.
func (ps *PasteStore) StoreAndFormat(content string) string {
	lineCount := CountLines(content)
	id := ps.Store(content)
	if id == 0 {
		return content
	}
	return FormatRef(id, lineCount)
}

// Get retrieves content by ID.
func (ps *PasteStore) Get(id int) (string, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	content, ok := ps.contents[id]
	return content, ok
}

// Clear removes all stored content.
func (ps *PasteStore) Clear() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.contents = make(map[int]string)
	ps.nextID = 1
}

// FormatRef returns a reference string for display.
// Example: "[Pasted text #1 +10 lines]"
func FormatRef(id int, numLines int) string {
	if numLines == 0 {
		return fmt.Sprintf("[Pasted text #%d]", id)
	}
	return fmt.Sprintf("[Pasted text #%d +%d lines]", id, numLines)
}

// NormalizeLineEndings converts \r\n and standalone \r to \n so that
// line-counting and display work correctly regardless of the source platform.
func NormalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// CountLines returns the number of lines in content (newlines + 1 if content is not empty).
func CountLines(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

// ExpandRefs expands all paste references in the input with actual content from the store.
// This should be called before sending the prompt to the agent.
func (ps *PasteStore) ExpandRefs(input string) string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	result := input
	for id, content := range ps.contents {
		// Try to match both formats
		refWithLines := fmt.Sprintf("[Pasted text #%d +", id)
		refSimple := fmt.Sprintf("[Pasted text #%d]", id)

		if idx := strings.Index(result, refWithLines); idx != -1 {
			// Find the closing bracket after the prefix
			if end := strings.Index(result[idx:], "]"); end != -1 {
				fullRef := result[idx : idx+end+1]
				result = strings.Replace(result, fullRef, content, 1)
			}
		} else {
			result = strings.Replace(result, refSimple, content, 1)
		}
	}

	return result
}
