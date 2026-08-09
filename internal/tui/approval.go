package tui

// showApproval activates the approval dialog for a single request.
// The run stopwatch pauses while the dialog is up (time waiting on the user
// is not agent time).
func (m *Model) showApproval(msg ToolApprovalRequestMsg) {
	m.beginRunPause()
	m.approvalPending = true
	m.approvalToolName = msg.Name
	m.approvalToolArgs = msg.Args
	m.approvalRespChan = msg.Resp
	m.approvalIsExternal = msg.IsExternal
	m.approvalIsBillable = msg.IsBillable
	m.approvalWorkerName = msg.WorkerName
	m.approvalWorkerColor = msg.WorkerColor
	m.approvalOptions = append(m.approvalOptions[:0], msg.Options...)
	m.approvalAllowApproveAll = msg.AllowApproveAll && len(m.approvalOptions) == 0
	m.approvalSelected = 0 // Default to "Approve"
	m.textarea.Blur()
	m.refreshViewport()
}

func (m *Model) approvalResponse(approved bool, responseMode ApprovalMode) ToolApprovalResponse {
	response := ToolApprovalResponse{Approved: approved, Mode: responseMode}
	wantedKind := "allow_once"
	if !approved {
		wantedKind = "deny"
	}
	for _, option := range m.approvalOptions {
		if option.Kind == wantedKind {
			response.ResolvedOptionID = option.ID
			break
		}
	}
	return response
}

// showApprovalPromotionFailure keeps the current request pending and surfaces
// only an opaque storage error. Local paths/details remain in the debug log.
func (m *Model) showApprovalPromotionFailure() {
	m.lines = append(m.lines, textLine("   "+toolErrorStyle.Render("⚠ Full access unchanged — could not save mode change")))
	m.refreshViewport()
}

// resolveApproval responds to the current approval dialog and, if there are
// queued requests, immediately shows the next one. When the user selects
// "Approve All" (ModeAuto), all queued requests are auto-approved at once.
func (m *Model) resolveApproval(resp ToolApprovalResponse) {
	m.approvalPending = false
	if m.approvalRespChan != nil {
		m.approvalRespChan <- resp
	}

	// If user chose "Approve All", auto-approve everything in the queue.
	if resp.Mode == ModeAuto {
		remaining := make([]ToolApprovalRequestMsg, 0, len(m.approvalQueue))
		for _, queued := range m.approvalQueue {
			if queued.AllowApproveAll && queued.Resp != nil {
				queued.Resp <- ToolApprovalResponse{Approved: true, Mode: ModeAuto}
			} else {
				remaining = append(remaining, queued)
			}
		}
		m.approvalQueue = remaining
		if len(m.approvalQueue) > 0 {
			next := m.approvalQueue[0]
			m.approvalQueue = m.approvalQueue[1:]
			m.showApproval(next)
			return
		}
		m.endRunPause()
		m.textarea.Focus()
		m.refreshViewport()
		return
	}

	// Show next queued approval (the pause stays open), or resume the run
	// stopwatch and restore input focus.
	if len(m.approvalQueue) > 0 {
		next := m.approvalQueue[0]
		m.approvalQueue = m.approvalQueue[1:]
		m.showApproval(next)
	} else {
		m.endRunPause()
		m.textarea.Focus()
		m.refreshViewport()
	}
}
