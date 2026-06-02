package tui

import (
	"fmt"

	"github.com/cnjack/jcode/internal/session"
)

// dirItem implements list.Item
type dirItem struct {
	title       string
	name        string
	desc        string
	isDirectory bool
	isSelectBtn bool
}

func (i dirItem) Title() string       { return i.title }
func (i dirItem) Description() string { return i.desc }
func (i dirItem) FilterValue() string { return i.title }

type modelItem struct {
	provider         string
	model            string
	title            string
	desc             string
	isCurrent        bool   // currently active model
	isAction         bool   // action item (e.g. "Add New Model")
	actionID         string // action identifier (e.g. "add_model", "manage_models")
	isProviderHeader bool   // provider group header
}

func (i modelItem) Title() string       { return i.title }
func (i modelItem) Description() string { return i.desc }
func (i modelItem) FilterValue() string {
	// Provider headers should not be filterable individually, but should match if any of their models match
	if i.isProviderHeader {
		return ""
	}
	return i.model + " " + i.title
}

// settingItem is used for the /setting menu
type settingItem struct {
	title string
	desc  string
	key   string // action key
}

func (i settingItem) Title() string       { return i.title }
func (i settingItem) Description() string { return i.desc }
func (i settingItem) FilterValue() string { return i.title }

// sessionListItem implements list.Item for session picking.
type sessionListItem struct {
	meta session.SessionMeta
}

func (i sessionListItem) Title() string {
	ts := i.meta.StartTime
	if len(ts) >= 16 {
		ts = ts[:16]
	}
	if i.meta.Title != "" {
		return fmt.Sprintf("%s  %s", ts, i.meta.Title)
	}
	return fmt.Sprintf("%s  %s / %s", ts, i.meta.Provider, i.meta.Model)
}
func (i sessionListItem) Description() string {
	if i.meta.Title != "" {
		return fmt.Sprintf("%s / %s  %s", i.meta.Provider, i.meta.Model, i.meta.UUID[:8])
	}
	return i.meta.UUID
}
func (i sessionListItem) FilterValue() string { return i.meta.StartTime + i.meta.UUID }

// sshAliasItem for the SSH alias picker
type sshAliasItem struct {
	title string
	desc  string
	addr  string
	path  string
	isNew bool // "Connect new SSH" option
}

func (i sshAliasItem) Title() string       { return i.title }
func (i sshAliasItem) Description() string { return i.desc }
func (i sshAliasItem) FilterValue() string { return i.title }

// channelItem for the channel management picker
type channelItem struct {
	title string
	desc  string
	key   string // channel ID (e.g. "wechat")
}

func (i channelItem) Title() string       { return i.title }
func (i channelItem) Description() string { return i.desc }
func (i channelItem) FilterValue() string { return i.title }
