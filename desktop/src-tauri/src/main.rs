// Prevents a stray console window on Windows in release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod desktop_apps;
mod dropped_files;
mod shell_env;
mod sidecar;
mod tray;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;

use tauri::{AppHandle, Emitter, Manager, RunEvent, WindowEvent};
use tauri_plugin_shell::process::CommandChild;

/// Holds the running jcode sidecar so we can terminate it explicitly on exit.
/// Tauri already best-effort kills spawned children, but a background of
/// loopback servers is exactly where a leaked process is most annoying, so we
/// own the lifecycle outright.
#[derive(Default)]
pub struct SidecarHandle(pub Mutex<Option<CommandChild>>);
/// Cross-cutting desktop state. `tray` records whether the menu-bar tray was
/// actually created — close-to-tray must only swallow the window's close when
/// there is a tray to reopen from, or the user would be stranded (e.g. on a
/// Linux desktop with no StatusNotifier host).
#[derive(Default)]
pub struct DesktopState {
    pub tray: AtomicBool,
}

const WINDOW_REOPENED_EVENT: &str = "jcode://window-reopened";

/// Bring the main window to the foreground (used by the tray and the
/// single-instance guard when a second launch is attempted).
pub fn show_main(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let was_visible = w.is_visible().unwrap_or(false);
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
        if !was_visible {
            let _ = app.emit(WINDOW_REOPENED_EVENT, ());
        }
    }
}

/// Toggle window visibility — the tray icon's left-click behaviour. A minimized
/// window still reports `is_visible() == true`, so treat "visible but minimized"
/// as "should be restored" rather than hiding it; otherwise a left-click on a
/// minimized window would hide it instead of bringing it forward.
pub fn toggle_main(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let visible = w.is_visible().unwrap_or(false);
        let minimized = w.is_minimized().unwrap_or(false);
        if visible && !minimized {
            let _ = w.hide();
        } else {
            show_main(app);
        }
    }
}

fn kill_sidecar(app: &AppHandle) {
    if let Some(state) = app.try_state::<SidecarHandle>() {
        if let Ok(mut guard) = state.0.lock() {
            if let Some(child) = guard.take() {
                let _ = child.kill();
            }
        }
    }
}

/// IPC command the frontend calls to learn the sidecar's loopback port. The
/// desktop shell serves the page itself (Tauri's built-in origin), so the page
/// is cross-origin to the Go API server and must build request URLs against
/// `http://127.0.0.1:<port>`. Returns the port once `sidecar::start` has picked
/// it, or `None` if the sidecar hasn't initialized yet — the frontend polls.
#[tauri::command]
fn get_sidecar_port(port: tauri::State<'_, sidecar::SidecarPort>) -> Option<u16> {
    port.0.lock().ok().and_then(|guard| *guard)
}

fn main() {
    let mut builder = tauri::Builder::default();

    // Single-instance must be the FIRST plugin so a second launch is short-
    // circuited before any window/sidecar work happens.
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            show_main(app);
        }));
    }

    builder = builder
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        // Relaunch support for the updater (swaps in the staged bundle).
        .plugin(tauri_plugin_process::init());

    #[cfg(desktop)]
    {
        builder = builder
            .plugin(tauri_plugin_window_state::Builder::default().build())
            // Registered without a shortcut here; the accelerator is bound in
            // setup() so a hotkey conflict logs instead of crashing the app.
            .plugin(tauri_plugin_global_shortcut::Builder::new().build())
            // Auto-updater: fetches the signed latest.json from GitHub Releases,
            // verifies against the pubkey in tauri.conf.json. The in-app flow
            // (banner → download → install → relaunch) lives in the web UI.
            .plugin(tauri_plugin_updater::Builder::new().build());
    }

    let app = builder
        .manage(SidecarHandle::default())
        .manage(sidecar::SidecarPort::default())
        .manage(DesktopState::default())
        .invoke_handler(tauri::generate_handler![
            get_sidecar_port,
            dropped_files::read_dropped_image,
            desktop_apps::list_workspace_applications,
            desktop_apps::open_workspace_in_application
        ])
        .setup(|app| {
            // Start the backend FIRST so a (possibly cosmetic) tray failure can
            // never prevent the server — and thus the whole app — from coming up.
            if let Err(e) = sidecar::start(app.handle()) {
                eprintln!("[jcode] failed to start sidecar: {e}");
                use tauri_plugin_dialog::DialogExt;
                app.handle()
                    .dialog()
                    .message(format!("jcode could not start its local server:\n{e}"))
                    .title("jcode")
                    .blocking_show();
                app.handle().exit(1);
                return Ok(());
            }

            // The tray is best-effort. On a Linux desktop without a tray host it
            // may fail; we log and fall back to "closing the window quits".
            match tray::create(app.handle()) {
                Ok(()) => {
                    if let Some(state) = app.try_state::<DesktopState>() {
                        state.tray.store(true, Ordering::Relaxed);
                    }
                }
                Err(e) => eprintln!("[jcode] tray unavailable, close will quit: {e}"),
            }

            // Global hotkey is a convenience; a conflict must not crash the app.
            #[cfg(desktop)]
            {
                use tauri_plugin_global_shortcut::{
                    Code, GlobalShortcutExt, Modifiers, Shortcut, ShortcutState,
                };
                // ⌘/⊞ + Shift + J toggles the window from anywhere.
                let toggle = Shortcut::new(Some(Modifiers::SUPER | Modifiers::SHIFT), Code::KeyJ);
                if let Err(e) = app.global_shortcut().on_shortcut(toggle, |app, _sc, event| {
                    if event.state == ShortcutState::Pressed {
                        toggle_main(app);
                    }
                }) {
                    eprintln!("[jcode] global shortcut not registered: {e}");
                }
            }

            Ok(())
        })
        .on_window_event(|window, event| {
            // Close-to-tray: when a tray icon exists, the main window's close
            // button hides it instead of quitting, so a long-running agent keeps
            // working in the background — reopen from the tray, the global
            // hotkey, or relaunching. With no tray, closing quits normally. A
            // true exit is always available via the tray "Quit" item, the macOS
            // app menu (Cmd+Q), or Alt+F4 / closing when there is no tray.
            if let WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    let tray_active = window
                        .app_handle()
                        .try_state::<DesktopState>()
                        .map(|s| s.tray.load(Ordering::Relaxed))
                        .unwrap_or(false);
                    if tray_active {
                        let _ = window.hide();
                        api.prevent_close();
                    }
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building jcode desktop");

    app.run(|app_handle, event| match event {
        RunEvent::ExitRequested { .. } | RunEvent::Exit => kill_sidecar(app_handle),
        // macOS sends Reopen when the Dock icon is clicked after the last
        // window was closed-to-tray. Route it through show_main so the WebView
        // receives the same New Task event as tray/single-instance reopening.
        #[cfg(target_os = "macos")]
        RunEvent::Reopen {
            has_visible_windows,
            ..
        } if !has_visible_windows => show_main(app_handle),
        _ => {}
    });
}
