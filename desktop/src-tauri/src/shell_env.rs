//! Resolve the user's real *login-shell* environment so the bundled `jcode`
//! sidecar — and the agent it runs — behaves exactly as if launched from the
//! user's terminal.
//!
//! GUI launches (macOS Dock/Finder/Spotlight/launchd, Linux desktop launchers)
//! inherit a *minimal* environment: launchd's bare `PATH`, no Homebrew
//! (`/opt/homebrew/bin`), none of the user's `~/.zprofile`/`~/.zshrc` additions
//! or version-manager shims (nvm, pyenv, rbenv). That silently breaks every tool
//! jcode shells out to — `rg` (grep/glob), `git`, node — because they aren't on
//! the degraded `PATH`. Terminals never hit this: they source the login shell
//! first. We reproduce that — run the user's shell as a login + interactive
//! shell, dump its environment between markers, and hand the whole thing to the
//! sidecar.
//!
//! **Why the full environment, not just `PATH`.** The sidecar runs an agent that
//! does real work on the user's behalf — including tasks that need the
//! credentials the user exports from their profile (e.g. cloud access keys). A
//! desktop-launched agent should have parity with a terminal-launched one, so it
//! gets the same environment a terminal would. This is a deliberate trust
//! decision: it is the user's own machine and the user's own agent.

use std::collections::HashMap;
use std::io::Read;
use std::process::{Command, Stdio};
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
/// blocks (a stuck `read`, an `ssh-add` passphrase prompt, a hung network mount
/// in nvm/pyenv init) must not wedge desktop startup — we time out, kill the
/// probe, and fall back to the inherited (degraded) environment.
const PROBE_TIMEOUT: Duration = Duration::from_secs(5);

/// Capture the login shell's environment as a `KEY -> VALUE` map.
///
/// Returns `None` when this doesn't apply (Windows) or the probe fails/times
/// out — callers then keep the inherited environment. Never panics, never leaks
/// the probe process.
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
    // users put their PATH / tool-manager setup. Between the two we reconstruct
    // the same environment an interactive terminal would have.
    let script = format!("printf '%s\\n' '{START}'; env; printf '%s\\n' '{END}'");

    // Spawn (not `output()`) so we retain the child and can kill it on timeout —
    // otherwise a blocked rc file would leave an orphaned shell running after we
    // give up, accumulating one per launch for affected users.
    let mut child = Command::new(&shell)
        .args(["-ilc", &script])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .ok()?;

    // Read stdout on a helper thread so `recv_timeout` can bound the wait; the
    // pipe closing (normal exit or our kill) ends the read and the thread.
    let mut stdout = child.stdout.take()?;
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let mut buf = Vec::new();
        let _ = stdout.read_to_end(&mut buf);
        let _ = tx.send(buf);
    });

    let bytes = match rx.recv_timeout(PROBE_TIMEOUT) {
        Ok(buf) => {
            let _ = child.wait(); // reap the finished probe
            buf
        }
        Err(_) => {
            let _ = child.kill(); // don't leak the blocked shell
            let _ = child.wait(); // reap the killed child (no zombie)
            return None;
        }
    };

    let text = String::from_utf8_lossy(&bytes);
    let body = text.split_once(START)?.1.split_once(END)?.0;

    let mut env = HashMap::new();
    for line in body.lines() {
        // A line without '=' is a continuation of a multiline value (rare) —
        // skip it rather than mis-parse.
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
