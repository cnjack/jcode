//! Resolve the user's real *login-shell* environment so the bundled `jcode`
//! sidecar behaves exactly as if it had been launched from the user's terminal.
//!
//! GUI launches (macOS Dock/Finder/Spotlight/launchd, Linux desktop launchers)
//! inherit a *minimal* environment: launchd's bare `PATH`, none of the user's
//! `~/.zprofile`/`~/.zshrc` additions, no Homebrew (`/opt/homebrew/bin`), no
//! version-manager shims (nvm, pyenv, rbenv, …). That silently breaks every
//! tool jcode shells out to — `rg`, `git`, node, formatters — because they are
//! not on the degraded `PATH`. Terminals never hit this: they source the login
//! shell first. We reproduce that here — run the user's shell as a login +
//! interactive shell, dump its environment between markers, and hand the whole
//! thing to the sidecar. This is deliberately general (not a `PATH`-only patch):
//! the sidecar gets the same environment the user would get in a terminal, so
//! anything exported from their profile — API keys, `GOPATH`, editor vars — is
//! present too, and future "works in the terminal, not when double-clicked"
//! gaps are covered by construction.

use std::collections::HashMap;
use std::process::Command;
use std::sync::mpsc;
use std::time::Duration;

/// Variables describing the probe shell's own *transient* state rather than the
/// user's environment. Passing these to the sidecar would be actively wrong —
/// `PWD`/`OLDPWD` would fight the `current_dir` we set, and `SHLVL`/`_` are
/// meaningless to inherit. Everything else is forwarded verbatim.
const SKIP: &[&str] = &["PWD", "OLDPWD", "SHLVL", "_"];

/// Markers bracketing the `env` dump so we can discard any login banner / rc
/// chatter the shell prints around it. Chosen to be vanishingly unlikely to
/// appear inside a real environment value.
const START: &str = "__JCODE_SHELL_ENV_START__";
const END: &str = "__JCODE_SHELL_ENV_END__";

/// Hard cap on how long we wait for the probe shell. A pathological rc file that
/// blocks on input must not wedge desktop startup, so we time out and fall back
/// to the inherited (degraded) environment rather than hang the splash forever.
const PROBE_TIMEOUT: Duration = Duration::from_secs(5);

/// Capture the login shell's environment as a `KEY -> VALUE` map.
///
/// Returns `None` when this doesn't apply (Windows) or the probe fails/times
/// out — callers then keep the inherited environment. Never panics.
pub fn login_shell_env() -> Option<HashMap<String, String>> {
    // Windows has no `$SHELL`/login-shell concept and doesn't suffer the
    // launchd PATH degradation, so there is nothing to repair.
    if cfg!(windows) {
        return None;
    }

    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());

    // `-l` (login) sources /etc/zprofile — which runs `path_helper`, pulling in
    // /etc/paths.d/* (Homebrew registers /opt/homebrew/bin there) — plus
    // ~/.zprofile. `-i` (interactive) additionally sources ~/.zshrc, where many
    // users put their PATH/tool-manager setup. Between the two we reconstruct
    // the same environment an interactive terminal would have.
    let script = format!("printf '%s\\n' '{START}'; env; printf '%s\\n' '{END}'");

    // Run the probe on a helper thread guarded by a timeout: `Command::output`
    // has no built-in timeout, and we must never let a hanging shell block
    // startup. On timeout the orphaned shell is left to exit on its own.
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let out = Command::new(&shell).args(["-ilc", &script]).output();
        let _ = tx.send(out);
    });
    let output = rx.recv_timeout(PROBE_TIMEOUT).ok()?.ok()?;

    let stdout = String::from_utf8_lossy(&output.stdout);
    let body = stdout.split_once(START)?.1.split_once(END)?.0;

    let mut env = HashMap::new();
    for line in body.lines() {
        // A line without '=' is a continuation of a multiline value (rare) —
        // skip it rather than mis-parse. PATH and friends are single-line, so
        // the tool-resolution fix is unaffected.
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        if key.is_empty() || SKIP.contains(&key) {
            continue;
        }
        env.insert(key.to_string(), value.to_string());
    }

    // A missing PATH means the probe produced nothing usable — treat the whole
    // capture as failed so the caller falls back cleanly.
    if env.contains_key("PATH") {
        Some(env)
    } else {
        None
    }
}
