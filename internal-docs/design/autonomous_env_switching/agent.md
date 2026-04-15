# Autonomous Environment Switching - Agent Design (KISS)

## 1. Goal
Provide the agent with a simple tool to switch its `Env` (Executor) between the `local` machine and user-configured `SSH aliases`.

## 2. Tool: `switch_env`

**Name**: `switch_env`  
**Description**: "Switch the execution environment between the local machine and SSH servers."

**Parameters Schema**:
```json
{
  "target": {
    "type": "string",
    "description": "The destination environment. Must be 'local' or an exact SSH alias name."
  }
}
```

**Returns**: 
A simple success/failure message. The tool itself handles updating the global `Env` mapping.
*Example*: `Switched to 'prod-db' (root@192.168.1.10:/root)`

## 3. System Prompt Injection
To let the agent know where it is and where it can go, we inject a minimal block into the prompt:

```markdown
Current Environment: {{.CurrentEnvLabel}}
Available target environments for 'switch_env' tool:
- local
{{range .SSHAliases}}- {{.Name}} ({{.Addr}}){{end}}

Note: The TUI displays your current environment to the user. Do not state "I will now switch to..." or "I have switched to...", just execute the tool and continue.
```

## 4. Execution Flow
1. Model calls `switch_env(target="prod-db")`.
2. Tool looks up `prod-db` in `config.SSHAliases`.
3. Tool connects and assigns the new `SSHExecutor` to the global `agent.Env`.
4. Tool emits an event to BubbleTea to update the UI StatusBar.
5. Model receives the short success return and proceeds with standard `execute` / `read_file` commands on the new environment.

## 5. Session Persistence & Resumes

When a user exits and later resumes a session, the current environment state must be preserved so the agent does not unexpectedly drop back to the `local` environment.

**Design:**
1. **Replaying State on Resume**: Because the session recorder logs all `EntryToolCall` and `EntryToolResult` events, the engine can scan the loaded history (`session.LoadSession`) for the **last successful `switch_env` tool execution**.
2. **Auto-Connecting**: If the last known successful environment wasn't `local` (e.g., an SSH alias), the core initialisation flow must extract that alias from the tool arguments and automatically invoke `Env.SetSSH` to re-establish the connection *before* resuming the main loop.
3. **Fallback handling**: If the SSH server is temporarily unreachable upon resume, the initialisation layer should log a warning, reset the environment to `local`, and inform the agent/user of the fallback.
4. **UI Synchronisation**: The BubbleTea TUI must query the initialised `Env` at startup so the Status Bar immediately reflects the correct loaded environment.
