//! Locating the System Settings window — the "positioning" half of the drag
//! affordance. The drag bar decides *whether to show itself* and *where* from
//! the on-screen position of the System Settings window, polled once per tick.
//!
//! CGWindowListCopyWindowInfo deliberately: owner PID, layer, and bounds are
//! readable with **zero TCC grants** (only window *names* need Screen
//! Recording), so the chicken-and-egg problem — needing a permission to guide
//! the user to the permission — never arises.

use std::ffi::c_void;

use objc2::rc::Retained;
use objc2_app_kit::NSRunningApplication;
use objc2_foundation::{ns_string, NSArray, NSDictionary, NSNumber, NSString};

#[link(name = "CoreGraphics", kind = "framework")]
extern "C" {
    fn CGWindowListCopyWindowInfo(option: u32, relative_to_window: u32) -> *mut c_void;
}

const ON_SCREEN_ONLY: u32 = 1 << 0;
const EXCLUDE_DESKTOP_ELEMENTS: u32 = 1 << 4;
const NULL_WINDOW_ID: u32 = 0;

fn settings_bundle_id() -> &'static NSString {
    ns_string!("com.apple.systempreferences")
}

/// Whether System Settings is the frontmost application. No TCC involved —
/// NSWorkspace's frontmost app is ordinary public API.
pub fn settings_is_frontmost() -> bool {
    let Some(front) = (unsafe { objc2_app_kit::NSWorkspace::sharedWorkspace().frontmostApplication() })
    else {
        return false;
    };
    match unsafe { front.bundleIdentifier() } {
        Some(id) => id.isEqualToString(settings_bundle_id()),
        None => false,
    }
}

/// System Settings' frontmost ordinary window, in CG global coordinates
/// (origin at the top-left of the primary display, y down).
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct SettingsWindow {
    pub x: f64,
    pub y: f64,
    pub w: f64,
    pub h: f64,
}

pub fn find_settings_window() -> Option<SettingsWindow> {
    let apps = unsafe {
        NSRunningApplication::runningApplicationsWithBundleIdentifier(settings_bundle_id())
    };
    if apps.is_empty() {
        return None;
    }
    let pids: Vec<i64> = apps.iter().map(|a| unsafe { a.processIdentifier() } as i64).collect();

    let raw = unsafe { CGWindowListCopyWindowInfo(ON_SCREEN_ONLY | EXCLUDE_DESKTOP_ELEMENTS, NULL_WINDOW_ID) };
    if raw.is_null() {
        return None;
    }
    // The Copy function returns +1; CFArray is toll-free bridged to NSArray.
    let list: Retained<NSArray<NSDictionary>> = unsafe { Retained::from_raw(raw.cast())? };

    // Front-to-back order; the first layer-0 window of the Settings process
    // that has a plausible pane size is the one the user is looking at.
    for info in list.iter() {
        let pid = match number_for(&info, ns_string!("kCGWindowOwnerPID")) {
            Some(n) => n.longLongValue(),
            None => continue,
        };
        if !pids.contains(&pid) {
            continue;
        }
        let layer = number_for(&info, ns_string!("kCGWindowLayer")).map(|n| n.longLongValue());
        if layer != Some(0) {
            continue;
        }
        let bounds = match info.objectForKey(ns_string!("kCGWindowBounds")) {
            Some(b) => b,
            None => continue,
        };
        let bounds = match bounds.downcast::<NSDictionary>() {
            Ok(b) => b,
            Err(_) => continue,
        };
        let get = |key: &NSString| number_for(&bounds, key).map(|n| n.doubleValue());
        let (Some(x), Some(y), Some(w), Some(h)) = (
            get(ns_string!("X")),
            get(ns_string!("Y")),
            get(ns_string!("Width")),
            get(ns_string!("Height")),
        ) else {
            continue;
        };
        // Filter out the menu-bar item / tiny auxiliary windows.
        if w < 400.0 || h < 300.0 {
            continue;
        }
        return Some(SettingsWindow { x, y, w, h });
    }
    None
}

fn number_for(dict: &NSDictionary, key: &NSString) -> Option<Retained<NSNumber>> {
    dict.objectForKey(key)?.downcast::<NSNumber>().ok()
}
