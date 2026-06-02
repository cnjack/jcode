package tui

// showApproval activates the approval dialog for a single request.
func (m *Model) showApproval(msg ToolApprovalRequestMsg) {
	m.approvalPending = true
	m.approvalToolName = msg.Name
	m.approvalToolArgs = msg.Args
	m.approvalRespChan = msg.Resp
	m.approvalIsExternal = msg.IsExternal
	m.approvalWorkerName = msg.WorkerName
	m.approvalWorkerColor = msg.WorkerColor
	m.approvalSelected = 0 // Default to "Approve"
	m.textarea.Blur()
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
		for _, queued := range m.approvalQueue {
			if queued.Resp != nil {
				queued.Resp <- ToolApprovalResponse{Approved: true, Mode: ModeAuto}
			}
		}
		m.approvalQueue = nil
		m.textarea.Focus()
		m.refreshViewport()
		return
	}

	// Show next queued approval, or restore input focus.
	if len(m.approvalQueue) > 0 {
		next := m.approvalQueue[0]
		m.approvalQueue = m.approvalQueue[1:]
		m.showApproval(next)
	} else {
		m.textarea.Focus()
		m.refreshViewport()
	}
}
