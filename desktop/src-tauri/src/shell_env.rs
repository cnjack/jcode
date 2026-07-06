//! Resolve the user's login-shell **PATH** so the bundled `jcode` sidecar can
//! find the CLI tools it shells out to.
//!
//! GUI launches (macOS Dock/Finder/Spotlight/launchd, Linux desktop launchers)
//! inherit a *minimal* environment: launchd's bare `PATH`, no Homebrew
//! (`/opt/homebrew/bin`), none of the user's `~/.zprofile`/`~/.zshrc` PATH
//! additions or version-manager shims (nvm, pyenv, rbenv). That silently breaks
//! every tool jcode resolves by name — `rg` (grep/glob), `git`, node — because
//! they aren't on the degraded `PATH`. Terminals never hit this: they source the
//! login shell first. We reproduce just that: ask the login shell for its
//! `PATH` and hand it to the sidecar.
//!
//! **Why PATH only, not the whole environment.** Tool resolution is purely a
//! `PATH` problem, and importing the *entire* login environment would drag the
//! user's profile-exported secrets (cloud access keys, API tokens, etc.) into
//! the sidecar's process environment. jcode runs an agent that can execute
//! arbitrary commands, read its own environment, and be steered by untrusted
//! input — so injecting credentials it doesn't need is a real exfiltration risk,
//! not a convenience. We deliberately take *only* `PATH`: it fully fixes tool
//! resolution and carries no secrets.

use std::io::Read;
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;

/// Markers bracketing the `PATH` value so we can discard any login banner / rc
/// chatter the shell prints around it. Chosen to be vanishingly unlikely to
/// appear inside a real `PATH`.
const START: &str = "__JCODE_PATH_START__";
const END: &str = "__JCODE_PATH_END__";

/// Hard cap on how long we wait for the probe shell. A pathological rc file that
/// blocks (a stuck `read`, an `ssh-add` passphrase prompt, a hung network mount
/// in nvm/pyenv init) must not wedge desktop startup — we time out, kill the
/// probe, and fall back to the inherited (degraded) `PATH`.
const PROBE_TIMEOUT: Duration = Duration::from_secs(5);

/// Resolve the user's login-shell `PATH`.
///
/// Returns `None` when this doesn't apply (Windows) or the probe fails/times
/// out — callers then keep the inherited `PATH`. Never panics, never leaks the
/// probe process.
pub fn login_shell_path() -> Option<String> {
    // Windows has no `$SHELL`/login-shell concept and doesn't suffer the
    // launchd PATH degradation, so there is nothing to repair.
    if cfg!(windows) {
        return None;
    }

    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());

    // `-l` (login) sources /etc/zprofile — which runs `path_helper`, pulling in
    // /etc/paths.d/* (Homebrew registers /opt/homebrew/bin there) — plus
    // ~/.zprofile. `-i` (interactive) additionally sources ~/.zshrc, where many
    // users put their PATH / tool-manager setup. We print only `$PATH`, bracketed
    // by markers; nothing else from the environment is read.
    let script = format!("printf '%s%s%s' '{START}' \"$PATH\" '{END}'");

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
        let mut buf = String::new();
        let _ = stdout.read_to_string(&mut buf);
        let _ = tx.send(buf);
    });

    let out = match rx.recv_timeout(PROBE_TIMEOUT) {
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

    let path = out.split_once(START)?.1.split_once(END)?.0.trim();
    if path.is_empty() {
        None
    } else {
        Some(path.to_string())
    }
}
