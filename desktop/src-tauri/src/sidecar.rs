//! Launches the bundled `jcode` binary as a Tauri sidecar and points the main
//! window at it once it is accepting connections.
//!
//! The Go binary already embeds the entire web UI (frontend + REST + WebSocket),
//! so the desktop app reuses that server verbatim: Rust just picks a free
//! loopback port, spawns `jcode web` on it, waits until the port is live, then
//! navigates the (initially hidden, splash-showing) window to the server.

use std::net::{SocketAddr, TcpListener, TcpStream};
use std::time::Duration;

use tauri::{AppHandle, Manager, Url};
use tauri_plugin_shell::process::CommandEvent;
use tauri_plugin_shell::ShellExt;

use crate::SidecarHandle;

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

pub fn start(app: &AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let port = pick_free_port();
    let url = format!("http://127.0.0.1:{port}");

    // Run the server from the user's home directory; the in-app workspace
    // picker takes over project selection from there.
    let workdir = app
        .path()
        .home_dir()
        .unwrap_or_else(|_| std::env::temp_dir());

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

    // Pump the sidecar's stdout/stderr into the desktop log so `jcode web`
    // diagnostics are still reachable when running headless inside the app.
    tauri::async_runtime::spawn(async move {
        while let Some(event) = rx.recv().await {
            match event {
                CommandEvent::Stdout(bytes) => {
                    eprintln!("[jcode] {}", String::from_utf8_lossy(&bytes).trim_end());
                }
                CommandEvent::Stderr(bytes) => {
                    eprintln!("[jcode] {}", String::from_utf8_lossy(&bytes).trim_end());
                }
                CommandEvent::Terminated(payload) => {
                    eprintln!("[jcode] sidecar exited: {payload:?}");
                }
                _ => {}
            }
        }
    });

    // Health-poll the port on a background thread, then reveal the window. We
    // verify the /api/health response (not just a bare TCP connect) so that if
    // another process grabbed the port in the moment between pick_free_port and
    // the sidecar binding it, we don't navigate the window to a foreign server.
    let app = app.clone();
    let addr: SocketAddr = ([127, 0, 0, 1], port).into();
    std::thread::spawn(move || {
        for _ in 0..400 {
            if health_ok(&addr, port) {
                if let Some(w) = app.get_webview_window("main") {
                    if let Ok(parsed) = Url::parse(&url) {
                        let _ = w.navigate(parsed);
                    }
                    let _ = w.show();
                    let _ = w.set_focus();
                }
                return;
            }
            std::thread::sleep(Duration::from_millis(150));
        }
        // Give up waiting after ~60s but still show the window (splash) so the
        // user sees the failure instead of an app that never appears.
        if let Some(w) = app.get_webview_window("main") {
            let _ = w.show();
        }
    });

    Ok(())
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
