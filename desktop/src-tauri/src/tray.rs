//! System-tray icon: left-click opens the window, right-click opens a menu
//! with show / hide / quit. The tray is what keeps jcode reachable after the
//! window is "closed" (hidden) to the background.
//!
//! The icon is a monochrome template image (black shapes on transparency);
//! macOS tints it automatically to match a light or dark menu bar, so it reads
//! as black on light and white on dark instead of the full-color app icon.

use tauri::image::Image;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Manager};

use crate::show_main;

/// Monochrome menu-bar icon, used as a macOS template image: the jcode logo
/// converted to grayscale (luminance -> ink coverage) on a transparent
/// background, so macOS tints it black on a light bar and white on a dark bar.
/// See `icons/tray-template.png`.
const TRAY_ICON: &[u8] = include_bytes!("../icons/tray-template.png");

pub fn create(app: &AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let show = MenuItem::with_id(app, "show", "显示 jcode", true, None::<&str>)?;
    let hide = MenuItem::with_id(app, "hide", "隐藏窗口", true, None::<&str>)?;
    let sep = PredefinedMenuItem::separator(app)?;
    let quit = MenuItem::with_id(app, "quit", "退出 jcode", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&show, &hide, &sep, &quit])?;

    let icon = Image::from_bytes(TRAY_ICON)?;

    TrayIconBuilder::with_id("main")
        .icon(icon)
        .icon_as_template(true)
        .tooltip("jcode")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => show_main(app),
            "hide" => {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.hide();
                }
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main(tray.app_handle());
            }
        })
        .build(app)?;

    Ok(())
}
