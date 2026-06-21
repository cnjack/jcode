//! Launches the bundled `jcode` binary as a Tauri sidecar and exposes its
//! loopback port to the frontend once it is accepting connections.
//!
//! The desktop shell serves the page itself (Tauri's built-in frontend), while
//! the Go binary owns the REST + WebSocket API. Rust picks a free loopback port,
//! spawns `jcode web --headless` on it, waits until /api/health answers, stores
//! the port in the managed `SidecarPort` state (so the frontend can resolve an
//! absolute `http://127.0.0.1:<port>` API base via the `get_sidecar_port` IPC
//! command), then reveals the window.

use std::collections::VecDeque;
use std::io::Write as _;
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use tauri::{AppHandle, Manager};
use tauri_plugin_shell::process::CommandEvent;
use tauri_plugin_shell::ShellExt;

use crate::SidecarHandle;

/// Holds the dynamic loopback port the sidecar bound to, so the frontend can
/// reach the API over an absolute `http://127.0.0.1:<port>` URL. The desktop
/// shell serves the page itself (Tauri's built-in origin), so the page is no
/// longer same-origin with the Go server — it needs this port to build request
/// URLs. `None` until the port is picked / the sidecar is healthy.
#[derive(Default)]
pub struct SidecarPort(pub Mutex<Option<u16>>);

/// How long we wait for the sidecar to answer /api/health before giving up.
/// 400 × 150ms ≈ 60s — generous, since first launch may compile-cache, scan
/// skills, and connect MCP servers before binding.
const HEALTH_POLL_ATTEMPTS: usize = 400;
const HEALTH_POLL_INTERVAL: Duration = Duration::from_millis(150);
/// Cap on retained sidecar log lines kept in memory for the failure dialog.
const RECENT_LINES: usize = 80;

/// Ask the OS for an unused loopback port. There is a tiny TOCTOU window
/// between dropping this listener and the sidecar binding it, which is
/// acceptable for a local developer tool; the health poll below tolerates a
/// slow or failed bind.
fn pick_free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|l| l.local_addr())
        .map(|a| a.port())
        .unwrap_or(8799)
}

/// Path to the persisted sidecar log. Lives in the app log dir so a crash that
/// happens before the window ever loads is still inspectable after the fact —
/// the GUI swallows the sidecar's stdout/stderr otherwise.
fn sidecar_log_path(app: &AppHandle) -> PathBuf {
    let dir = app
        .path()
        .app_log_dir()
        .unwrap_or_else(|_| std::env::temp_dir());
    let _ = std::fs::create_dir_all(&dir);
    dir.join("jcode-sidecar.log")
}

pub fn start(app: &AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let port = pick_free_port();

    // Publish the port to managed state immediately so a fast-rendering
    // frontend's `get_sidecar_port` IPC call can resolve it without waiting for
    // the health poll — the actual API won't answer until health passes, but
    // the port itself is known as soon as we spawn.
    if let Some(state) = app.try_state::<SidecarPort>() {
        if let Ok(mut guard) = state.0.lock() {
            *guard = Some(port);
        }
    }

    // Run the server from the user's home directory; the in-app workspace
    // picker takes over project selection from there.
    let workdir = app
        .path()
        .home_dir()
        .unwrap_or_else(|_| std::env::temp_dir());

    let log_path = sidecar_log_path(app);

    let (mut rx, child) = app
        .shell()
        .sidecar("jcode")?
        .args([
            "web",
            "--port",
            &port.to_string(),
            "--host",
            "127.0.0.1",
            "--open=false",
        ])
        .current_dir(workdir)
        .spawn()?;

    if let Some(state) = app.try_state::<SidecarHandle>() {
        if let Ok(mut guard) = state.0.lock() {
            *guard = Some(child);
        }
    }

    // `ready` flips true once the sidecar's /api/health answers. Until then, the
    // sidecar exiting is a fatal *startup* failure that we surface to the user —
    // previously such a crash left the splash spinning forever, which is exactly
    // the "stuck on 正在启动本地服务" symptom.
    let ready = Arc::new(AtomicBool::new(false));
    // Ring buffer of the sidecar's most recent output lines, shown in the
    // failure dialog so the user (or a bug report) captures the real panic.
    let recent = Arc::new(Mutex::new(VecDeque::<String>::with_capacity(RECENT_LINES)));

    // Pump the sidecar's stdout/stderr into the desktop log AND a persistent log
    // file AND the in-memory ring buffer, so `jcode web` diagnostics survive the
    // headless GUI context. On an early exit, raise the failure dialog.
    let pump_app = app.clone();
    let pump_ready = ready.clone();
    let pump_recent = recent.clone();
    let pump_log = log_path.clone();
    tauri::async_runtime::spawn(async move {
        let mut file = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&pump_log)
            .ok();

        let mut record = |line: String| {
            eprintln!("[jcode] {line}");
            if let Some(f) = file.as_mut() {
                let _ = writeln!(f, "{line}");
            }
            if let Ok(mut buf) = pump_recent.lock() {
                if buf.len() >= RECENT_LINES {
                    buf.pop_front();
                }
                buf.push_back(line);
            }
        };

        while let Some(event) = rx.recv().await {
            match event {
                CommandEvent::Stdout(bytes) => {
                    record(String::from_utf8_lossy(&bytes).trim_end().to_string());
                }
                CommandEvent::Stderr(bytes) => {
                    record(String::from_utf8_lossy(&bytes).trim_end().to_string());
                }
                CommandEvent::Terminated(payload) => {
                    record(format!(
                        "sidecar exited before ready: code={:?} signal={:?}",
                        payload.code, payload.signal
                    ));
                    if !pump_ready.load(Ordering::SeqCst) {
                        surface_startup_failure(&pump_app, &pump_recent, &pump_log, payload.code);
                    }
                    break;
                }
                _ => {}
            }
        }
    });

    // Health-poll the port on a background thread, then reveal the window. We
    // verify the /api/health response (not just a bare TCP connect) so that if
    // another process grabbed the port in the moment between pick_free_port and
    // the sidecar binding it, the frontend won't be pointed at a foreign server.
    // The window stays on Tauri's built-in page (the splash / app shell); it is
    // NOT navigated to the sidecar — the page reaches the API via an absolute
    // `http://127.0.0.1:<port>` URL resolved from `get_sidecar_port`.
    let app = app.clone();
    let addr: SocketAddr = ([127, 0, 0, 1], port).into();
    let poll_ready = ready.clone();
    std::thread::spawn(move || {
        for _ in 0..HEALTH_POLL_ATTEMPTS {
            // The sidecar already died (and the pump surfaced it) — stop polling.
            if poll_ready.load(Ordering::SeqCst) {
                return;
            }
            if health_ok(&addr, port) {
                poll_ready.store(true, Ordering::SeqCst);
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.show();
                    let _ = w.set_focus();
                }
                return;
            }
            std::thread::sleep(HEALTH_POLL_INTERVAL);
        }
        // Timed out while the sidecar is (apparently) still alive but never
        // became healthy. Surface it rather than leaving the splash forever.
        if !poll_ready.swap(true, Ordering::SeqCst) {
            if let Some(w) = app.get_webview_window("main") {
                let _ = w.show();
            }
            surface_startup_failure(&app, &recent, &log_path, None);
        }
    });

    Ok(())
}

/// Show a blocking error dialog describing why the local server never came up,
/// then quit. Runs on its own thread so the blocking dialog doesn't wedge the
/// async pump or the event loop. Includes the tail of the sidecar output and
/// the log path so the failure is actionable instead of a silent spinner.
fn surface_startup_failure(
    app: &AppHandle,
    recent: &Arc<Mutex<VecDeque<String>>>,
    log_path: &std::path::Path,
    code: Option<i32>,
) {
    let tail: Vec<String> = recent
        .lock()
        .map(|b| b.iter().rev().take(14).rev().cloned().collect())
        .unwrap_or_default();

    let mut msg = String::from("jcode's local server stopped before it finished starting.\n\n");
    if let Some(c) = code {
        msg.push_str(&format!("Sidecar exit code: {c}\n"));
    }
    msg.push_str(&format!("Log: {}\n", log_path.display()));
    if !tail.is_empty() {
        msg.push_str("\nRecent output:\n");
        msg.push_str(&tail.join("\n"));
    }

    let app = app.clone();
    std::thread::spawn(move || {
        use tauri_plugin_dialog::DialogExt;
        app.dialog()
            .message(msg)
            .title("jcode failed to start")
            .blocking_show();
        app.exit(1);
    });
}

/// Probe GET /api/health and confirm it's actually our jcode server: a 200
/// response whose body carries the health JSON ("status" field). A foreign
/// listener that happened to grab the port won't satisfy both.
fn health_ok(addr: &SocketAddr, port: u16) -> bool {
    use std::io::{Read, Write};

    let Ok(mut stream) = TcpStream::connect_timeout(addr, Duration::from_millis(300)) else {
        return false;
    };
    let _ = stream.set_read_timeout(Some(Duration::from_millis(600)));
    let _ = stream.set_write_timeout(Some(Duration::from_millis(600)));

    let req = format!(
        "GET /api/health HTTP/1.0\r\nHost: 127.0.0.1:{port}\r\nConnection: close\r\n\r\n"
    );
    if stream.write_all(req.as_bytes()).is_err() {
        return false;
    }

    let mut buf = Vec::with_capacity(2048);
    let mut chunk = [0u8; 2048];
    loop {
        match stream.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => {
                buf.extend_from_slice(&chunk[..n]);
                if buf.len() > 8192 {
                    break;
                }
            }
            Err(_) => break,
        }
    }

    let resp = String::from_utf8_lossy(&buf);
    resp.starts_with("HTTP/1.") && resp.contains(" 200 ") && resp.contains("\"status\"")
}
