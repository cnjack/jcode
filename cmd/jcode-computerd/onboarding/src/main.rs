//! jcode-computerd-onboarding — permission onboarding UI + icon renderer for
//! the computer-use helper bundle. See Cargo.toml for why this exists as its
//! own executable inside jcode-computerd.app.
//!
//! Modes:
//!   (no args)                 show the onboarding UI (macOS only)
//!   --render-icon <dir>       write the Apple .iconset PNGs and exit
//!   --render-preview <file>   write a single 512 px preview PNG and exit
//!   --state                   print TCC grant state as JSON and exit
//!   --demo                    show the UI as a fresh user would see it, and
//!                             never auto-exit (design iteration)
//!   --demo-shot <dir>         render dialog.png + dragbar.png and exit

// objc2 marks many AppKit methods safe; the remaining `unsafe { }` blocks
// around the rest also mark every ObjC crossing uniformly. Keeping the
// uniform style beats churning blocks whenever a binding's safety changes.
#![allow(unused_unsafe)]

mod icon;
mod strings;

#[cfg(target_os = "macos")]
mod locator;
#[cfg(target_os = "macos")]
mod tcc;
#[cfg(target_os = "macos")]
mod ui;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.first().map(String::as_str) {
        Some("--render-icon") => {
            let dir = args.get(1).map(String::as_str).unwrap_or_else(|| {
                eprintln!("usage: jcode-computerd-onboarding --render-icon <dir>");
                std::process::exit(2);
            });
            if let Err(e) = icon::render_iconset(std::path::Path::new(dir)) {
                eprintln!("render-icon: {e}");
                std::process::exit(1);
            }
        }
        Some("--render-preview") => {
            let file = args.get(1).map(String::as_str).unwrap_or_else(|| {
                eprintln!("usage: jcode-computerd-onboarding --render-preview <file>");
                std::process::exit(2);
            });
            if let Err(e) = icon::render_single(std::path::Path::new(file), 512) {
                eprintln!("render-preview: {e}");
                std::process::exit(1);
            }
        }
        Some("--state") => state(),
        Some("--probe") => probe(),
        Some("--demo") => gui(GuiMode::Demo),
        Some("--demo-shot") => {
            let dir = args.get(1).cloned().unwrap_or_else(|| {
                eprintln!("usage: jcode-computerd-onboarding --demo-shot <dir>");
                std::process::exit(2);
            });
            gui(GuiMode::Shot(dir));
        }
        Some(other) => {
            eprintln!("unknown argument: {other}");
            std::process::exit(2);
        }
        None => gui(GuiMode::Normal),
    }
}

enum GuiMode {
    Normal,
    Demo,
    Shot(String),
}

#[cfg(target_os = "macos")]
fn state() {
    println!(
        "{{\"accessibility\":{},\"screen_recording\":{}}}",
        tcc::accessibility_granted(),
        tcc::screen_recording_granted()
    );
}

#[cfg(not(target_os = "macos"))]
fn state() {
    eprintln!("--state is macOS only");
    std::process::exit(1);
}

/// Dev diagnosis for the drag bar's visibility decision: every input the
/// tick uses, as one JSON line.
#[cfg(target_os = "macos")]
fn probe() {
    let win = locator::find_settings_window();
    println!(
        "{{\"accessibility\":{},\"screen_recording\":{},\"settings_frontmost\":{},\"settings_window\":{}}}",
        tcc::accessibility_granted(),
        tcc::screen_recording_granted(),
        locator::settings_is_frontmost(),
        match win {
            Some(w) => format!(
                "{{\"x\":{},\"y\":{},\"w\":{},\"h\":{}}}",
                w.x, w.y, w.w, w.h
            ),
            None => "null".to_string(),
        }
    );
}

#[cfg(not(target_os = "macos"))]
fn probe() {
    eprintln!("--probe is macOS only");
    std::process::exit(1);
}

#[cfg(target_os = "macos")]
fn gui(mode: GuiMode) {
    // One onboarding window at a time, across however many jcode processes
    // are running. The daemon already guards its own spawns; this closes the
    // cross-daemon race. O_EXLOCK|O_NONBLOCK takes the flock atomically at
    // open time; the lock dies with the process, so no stale-lock handling.
    // Demo/shot runs skip the lock — they are dev tooling, not the ceremony.
    if matches!(mode, GuiMode::Normal) {
        use std::os::unix::fs::OpenOptionsExt;
        // Env override exists for tests, which need a private lock so runs
        // don't collide with a real ceremony already on screen.
        let lock_path = std::env::var_os("JCODE_COMPUTERD_ONBOARDING_LOCK")
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| std::env::temp_dir().join("jcode-computerd-onboarding.lock"));
        const O_NONBLOCK: i32 = 0x0004;
        const O_EXLOCK: i32 = 0x0020;
        match std::fs::OpenOptions::new()
            .create(true)
            .write(true)
            .custom_flags(O_EXLOCK | O_NONBLOCK)
            .open(&lock_path)
        {
            Ok(lock) => {
                // Hold the flock for the process lifetime.
                std::mem::forget(lock);
            }
            Err(e) if e.raw_os_error() == Some(35) /* EWOULDBLOCK */ => return,
            // Lock trouble is not worth blocking the ceremony over.
            Err(_) => {}
        }
    }

    let langs = preferred_languages();
    let s = strings::pick(&langs);
    let options = match mode {
        GuiMode::Normal => ui::RunOptions::default(),
        GuiMode::Demo => ui::RunOptions { demo: true, shot_dir: None },
        GuiMode::Shot(dir) => ui::RunOptions {
            demo: true,
            shot_dir: Some(std::path::PathBuf::from(dir)),
        },
    };
    ui::run(s, options);
}

#[cfg(target_os = "macos")]
fn preferred_languages() -> Vec<String> {
    use objc2_foundation::NSLocale;
    unsafe { NSLocale::preferredLanguages() }
        .iter()
        .map(|l| l.to_string())
        .collect()
}

#[cfg(not(target_os = "macos"))]
fn gui(_mode: GuiMode) {
    eprintln!("the onboarding UI is macOS only (icon rendering works anywhere)");
    std::process::exit(1);
}
