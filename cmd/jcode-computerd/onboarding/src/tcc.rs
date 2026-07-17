//! TCC state probes + point-of-need prompts.
//!
//! This process runs inside jcode-computerd.app, so every call here is
//! attributed to the helper bundle's code identity — the same identity the
//! Swift daemon (Accessibility) and capture worker (Screen Recording) run
//! under. That shared attribution is the entire reason the onboarding UI is a
//! third executable in the bundle instead of a window in jcode itself.

use std::ffi::c_void;

use objc2_foundation::{ns_string, NSDictionary, NSNumber, NSString};

#[link(name = "ApplicationServices", kind = "framework")]
extern "C" {
    // Boolean (unsigned char), not C bool — compare against 0 explicitly.
    fn AXIsProcessTrusted() -> u8;
    fn AXIsProcessTrustedWithOptions(options: *const c_void) -> u8;
    static kAXTrustedCheckOptionPrompt: *const c_void; // CFStringRef
}

#[link(name = "CoreGraphics", kind = "framework")]
extern "C" {
    fn CGPreflightScreenCaptureAccess() -> bool;
    fn CGRequestScreenCaptureAccess() -> bool;
}

pub fn accessibility_granted() -> bool {
    unsafe { AXIsProcessTrusted() != 0 }
}

pub fn screen_recording_granted() -> bool {
    unsafe { CGPreflightScreenCaptureAccess() }
}

/// Fire the "would like to control this computer" consent prompt (and register
/// the bundle as a toggled-off row in the Accessibility list). Asynchronous
/// and idempotent — macOS never stacks duplicate alerts.
pub fn request_accessibility() {
    unsafe {
        // kAXTrustedCheckOptionPrompt is a CFString; toll-free bridge it to
        // NSString so the options dictionary can be built without dropping to
        // the CFDictionary C API. The dictionary itself bridges back to the
        // CFDictionaryRef parameter.
        let key: &NSString = &*(kAXTrustedCheckOptionPrompt as *const NSString);
        let value = NSNumber::new_bool(true);
        let options = NSDictionary::from_slices::<NSString>(&[key], &[value.as_ref()]);
        let _ = AXIsProcessTrustedWithOptions(
            options.as_ref() as *const NSDictionary<NSString, NSNumber> as *const c_void,
        );
    }
}

/// Fire the Screen Recording consent prompt (first time) / register the row.
pub fn request_screen_recording() {
    unsafe {
        let _ = CGRequestScreenCaptureAccess();
    }
}

/// Deep links into the exact System Settings panes. Used after the request
/// call: if the grant was previously denied macOS shows no second alert, so
/// landing the user on the right pane is the only path forward.
pub fn accessibility_pane() -> &'static NSString {
    ns_string!("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
}
pub fn screen_recording_pane() -> &'static NSString {
    ns_string!("x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")
}
