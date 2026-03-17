# Autonomous Environment Switching - UI Design (KISS)

## 1. Goal
Provide immediate, clear visual feedback to the user about which environment the agent is currently operating in, without cluttering the interface.

## 2. Header Status Display
The most prominent place to show the global execution context is the Header area (the very top of the TUI).
Instead of just a static title, the Header will dynamically reflect the `Env` state.

**Example Layouts:**
- **Local (Default)**: `🚀 Little Jack — Coding Assistant  |  🖥️  Env: Local`
- **Connected (SSH)**: `🚀 Little Jack — Coding Assistant  |  🔗 Env: SSH (prod-db)`

## 3. Environment States & Transitions
The UI needs to gracefully handle the transition when the agent calls `switch_env`:

1. **Active/Idle (Local or SSH)**: Header shows the static badge indicating current environment.
2. **Switching (In Progress)**: When the tool is executing, the Chat viewport or StatusBar shows a transient spinner: `[🌀 Switching environment to prod-db...]`.
3. **Success**: 
   - A system message is appended to the chat: `✅ Agent switched to prod-db (root@192.168.1.10)`.
   - The Header updates immediately to display the new environment badge.
4. **Error/Fallback**:
   - If the SSH connection fails (e.g., timeout, bad key), a system error message is appended: `❌ Failed to switch to prod-db (connection timeout). Reverted to Local.`
   - Header remains at the previous state (e.g., `Local`).

## 4. Approval & Safety
Even in a KISS design, silently jumping to another machine is risky.
- **Middleware Interception**: The Agent's `ApprovalFunc` will intercept the `switch_env` tool call.
- **User Confirmation**: The UI will prompt: `⚠️ Agent wishes to switch environment to [prod-db]. Allow? (y/N)`. 
- **Auto-Approve Exception**: If the user has "Auto-Approve" toggled ON, they take on the responsibility, and the agent will switch without the (y/N) prompt, acting completely autonomously.
