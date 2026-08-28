package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newRenameTestModel builds a Model with a textarea and a stub TitleController
// that records calls.
func newRenameTestModel(suggested string, suggestErr error, saveErr error) (*Model, *[]string) {
	saved := &[]string{}
	m := &Model{textarea: newTextarea()}
	m.titleCtl = &TitleController{
		Current: func() string { return "当前标题" },
		Suggest: func(context.Context) (string, error) { return suggested, suggestErr },
		Save: func(title string) (string, error) {
			if saveErr != nil {
				return "", saveErr
			}
			*saved = append(*saved, title)
			return title, nil
		},
	}
	return m, saved
}

// drainForTitleSuggested executes a (possibly batched) command tree and
// returns the first TitleSuggestedMsg, mirroring drainForPromptSubmit.
func drainForTitleSuggested(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case TitleSuggestedMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if found := drainForTitleSuggested(c); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestRenameCommandInSlashMenu(t *testing.T) {
	m := Model{}
	found := false
	for _, c := range m.getAllCommands() {
		if c.cmd == "/rename" {
			found = true
		}
	}
	if !found {
		t.Fatal("/rename should appear in the slash command menu")
	}
}

func TestRenameOpensEditorAndSeedsSuggestion(t *testing.T) {
	m, saved := newRenameTestModel("基于会话的建议标题", nil, nil)
	_, cmd := m.handleRenameInput("/rename", nil)
	if !m.renameActive {
		t.Fatal("/rename should open the editor")
	}
	if m.textarea.Value() != "当前标题" {
		t.Fatalf("editor should seed with current title, got %q", m.textarea.Value())
	}
	if len(*saved) != 0 {
		t.Fatal("opening the editor must not save anything")
	}

	// The suggestion command resolves to TitleSuggestedMsg and seeds the editor.
	msg, ok := drainForTitleSuggested(cmd).(TitleSuggestedMsg)
	if !ok {
		t.Fatalf("expected TitleSuggestedMsg from command, got %+v", msg)
	}
	m2, _ := m.handleTitleSuggested(msg, nil)
	mm := m2.(*Model)
	if mm.textarea.Value() != "基于会话的建议标题" {
		t.Fatalf("suggestion should seed the editor, got %q", mm.textarea.Value())
	}
	if mm.renameSuggesting {
		t.Fatal("suggesting flag should clear after the result lands")
	}
}

func TestRenameSuggestionFailureKeepsCurrentTitle(t *testing.T) {
	m, _ := newRenameTestModel("", errors.New("no small model configured"), nil)
	_, cmd := m.handleRenameInput("/rename", nil)
	msg, ok := drainForTitleSuggested(cmd).(TitleSuggestedMsg)
	if !ok {
		t.Fatalf("expected TitleSuggestedMsg, got %+v", msg)
	}
	m2, _ := m.handleTitleSuggested(msg, nil)
	mm := m2.(*Model)
	if !mm.renameActive {
		t.Fatal("editor should stay open on suggestion failure")
	}
	if mm.textarea.Value() != "当前标题" {
		t.Fatalf("failed suggestion must keep the current title, got %q", mm.textarea.Value())
	}
	if mm.renameNotice == "" || !strings.Contains(mm.renameNotice, "no small model") {
		t.Fatalf("failure notice should explain the reason, got %q", mm.renameNotice)
	}
}

func TestRenameLateSuggestionDoesNotClobberTyping(t *testing.T) {
	m, _ := newRenameTestModel("迟到的建议", nil, nil)
	_, cmd := m.handleRenameInput("/rename", nil)
	// User types before the suggestion lands.
	m.textarea.SetValue("用户自己输入的标题")
	m.handleRenameKey("x", tea.KeyPressMsg{Code: 'x'}, nil)
	if !m.renameEdited {
		t.Fatal("typing should mark the editor user-edited")
	}

	msg, _ := drainForTitleSuggested(cmd).(TitleSuggestedMsg)
	m2, _ := m.handleTitleSuggested(msg, nil)
	mm := m2.(*Model)
	if mm.textarea.Value() != "用户自己输入的标题" {
		t.Fatalf("late suggestion clobbered user input: %q", mm.textarea.Value())
	}
}

func TestRenameSuggestionAfterCloseIgnored(t *testing.T) {
	m, _ := newRenameTestModel("迟到的建议", nil, nil)
	_, cmd := m.handleRenameInput("/rename", nil)
	m.closeRenameEditor()

	msg, _ := drainForTitleSuggested(cmd).(TitleSuggestedMsg)
	m2, _ := m.handleTitleSuggested(msg, nil)
	mm := m2.(*Model)
	if mm.renameActive {
		t.Fatal("closed editor must not reopen on a late suggestion")
	}
	if mm.textarea.Value() != "" {
		t.Fatalf("late suggestion leaked into the prompt input: %q", mm.textarea.Value())
	}
}

func TestRenameEnterSavesAndEscCancels(t *testing.T) {
	// Enter saves the editor value.
	m, saved := newRenameTestModel("建议", nil, nil)
	m.handleRenameInput("/rename", nil)
	m.textarea.SetValue("确认后的标题")
	m.handleRenameKey("enter", tea.KeyPressMsg{Code: tea.KeyEnter}, nil)
	if m.renameActive {
		t.Fatal("editor should close after save")
	}
	if len(*saved) != 1 || (*saved)[0] != "确认后的标题" {
		t.Fatalf("save not called with editor value: %v", *saved)
	}

	// Esc cancels without saving.
	m2, saved2 := newRenameTestModel("建议", nil, nil)
	m2.handleRenameInput("/rename", nil)
	m2.textarea.SetValue("不会保存的标题")
	m2.handleRenameKey("esc", tea.KeyPressMsg{Code: tea.KeyEscape}, nil)
	if m2.renameActive {
		t.Fatal("editor should close after esc")
	}
	if len(*saved2) != 0 {
		t.Fatalf("esc must not save, got %v", *saved2)
	}

	// Empty enter is a clean no-op, not an error.
	m3, saved3 := newRenameTestModel("建议", nil, nil)
	m3.handleRenameInput("/rename", nil)
	m3.textarea.Reset()
	m3.handleRenameKey("enter", tea.KeyPressMsg{Code: tea.KeyEnter}, nil)
	if m3.renameActive {
		t.Fatal("editor should close on empty enter")
	}
	if len(*saved3) != 0 {
		t.Fatalf("empty enter must not save, got %v", *saved3)
	}
}

func TestRenameDirectArgumentSavesImmediately(t *testing.T) {
	m, saved := newRenameTestModel("", nil, nil)
	m.handleRenameInput("/rename 直接改名", nil)
	if m.renameActive {
		t.Fatal("/rename <title> should not open the editor")
	}
	if len(*saved) != 1 || (*saved)[0] != "直接改名" {
		t.Fatalf("direct rename not saved: %v", *saved)
	}
}

func TestRenameSaveErrorSurfacesFeedback(t *testing.T) {
	m, _ := newRenameTestModel("", nil, errors.New("session changed"))
	m.handleRenameInput("/rename 新名字", nil)
	if m.renameActive {
		t.Fatal("failed save should still close the editor")
	}
	last := m.lines[len(m.lines)-1].text
	if !strings.Contains(last, "session changed") {
		t.Fatalf("save error should surface in the transcript, last line: %q", last)
	}
}

func TestRenameWithoutControllerIsGraceful(t *testing.T) {
	m := &Model{textarea: newTextarea()}
	m.handleRenameInput("/rename", nil)
	if m.renameActive {
		t.Fatal("no controller must not open the editor")
	}
	if len(m.lines) == 0 || !strings.Contains(m.lines[len(m.lines)-1].text, "unavailable") {
		t.Fatalf("expected unavailable feedback, lines: %+v", m.lines)
	}
}
