//! The onboarding windows, in AppKit via objc2.
//!
//! Two windows, one policy:
//!
//! - **Dialog** — "Enable jcode Computer Use": the icon, one sentence of why,
//!   and an Allow row per grant. Allow fires the real TCC prompt *from this
//!   process* (helper-bundle identity) and deep-links the exact Settings pane.
//! - **Drag bar** — a floating panel that exists only while (a) Accessibility
//!   is still missing and (b) the System Settings window is on screen. It
//!   re-derives both facts every tick from the Settings window's position
//!   (locator.rs) and shows, hides, and re-anchors itself accordingly; the
//!   chip inside is an NSDraggingSource carrying the .app's file URL, so the
//!   user can drag the helper straight into the Accessibility list when a
//!   previously-denied grant means macOS won't re-prompt.

use std::cell::Cell;
use std::path::{Path, PathBuf};
use std::time::Instant;

use objc2::rc::Retained;
use objc2::runtime::{AnyObject, ProtocolObject};
use objc2::{
    define_class, msg_send, sel, AllocAnyThread, DefinedClass, MainThreadMarker, MainThreadOnly,
    Message,
};
use objc2_app_kit::{
    NSApplication, NSApplicationActivationPolicy, NSBackingStoreType, NSBox, NSBoxType, NSButton,
    NSColor, NSDragOperation, NSDraggingContext, NSDraggingItem, NSDraggingSession,
    NSDraggingSource, NSEvent, NSFont, NSImage, NSImageScaling, NSImageView, NSPanel,
    NSPasteboardWriting, NSRunningApplication, NSScreen, NSTextAlignment, NSTextField,
    NSTitlePosition, NSView, NSVisualEffectBlendingMode, NSVisualEffectMaterial,
    NSVisualEffectState, NSVisualEffectView, NSWindow, NSWindowCollectionBehavior,
    NSWindowDelegate, NSWindowStyleMask, NSWorkspace,
};
use objc2_foundation::{
    ns_string, NSArray, NSNotification, NSObject, NSObjectProtocol, NSPoint, NSRect, NSSize,
    NSString, NSTimer, NSURL,
};

use crate::locator::find_settings_window;
use crate::strings::Strings;
use crate::tcc;

/// How to run (see main.rs). `demo` keeps the windows up regardless of grant
/// state so the UI can be eyeballed on a machine where everything is already
/// authorized; `shot_dir` renders both windows to PNG and exits — pure dev
/// tooling for design iteration, no Screen Recording grant involved
/// (`cacheDisplayInRect` draws our own view hierarchy).
#[derive(Default)]
pub struct RunOptions {
    pub demo: bool,
    pub shot_dir: Option<PathBuf>,
}

const DIALOG_W: f64 = 720.0;
const DIALOG_H: f64 = 480.0;
const PANEL_W: f64 = 640.0;
const PANEL_H: f64 = 140.0;

/// What the drag chip drags, and what the dialog shows.
struct Identity {
    /// The thing to drag into the TCC list: the .app bundle root when running
    /// from inside one, the bare executable otherwise (dev runs).
    drag_target: PathBuf,
    /// Committed icon inside the bundle's Resources, when present.
    icns: Option<PathBuf>,
}

fn identity() -> Identity {
    let exe = std::env::current_exe().unwrap_or_else(|_| PathBuf::from("/"));
    let in_bundle = exe
        .parent()
        .map(|p| p.ends_with(Path::new("Contents/MacOS")))
        .unwrap_or(false);
    if in_bundle {
        // …/jcode-computerd.app/Contents/MacOS/<exe> → the .app root.
        let app = exe.ancestors().nth(3).map(Path::to_path_buf);
        if let Some(app) = app {
            let icns = app.join("Contents/Resources/jcode-computer-use.icns");
            return Identity {
                drag_target: app,
                icns: icns.exists().then_some(icns),
            };
        }
    }
    Identity { drag_target: exe, icns: None }
}

fn ns_path(p: &Path) -> Retained<NSString> {
    NSString::from_str(&p.to_string_lossy())
}

fn app_icon(identity: &Identity) -> Retained<NSImage> {
    if let Some(icns) = &identity.icns {
        if let Some(img) = NSImage::initWithContentsOfFile(NSImage::alloc(), &ns_path(icns)) {
            return img;
        }
    }
    // Always returns *something* (generic app icon at worst).
    unsafe { NSWorkspace::sharedWorkspace().iconForFile(&ns_path(&identity.drag_target)) }
}

fn open_pane(url: &NSString) {
    if let Some(url) = unsafe { NSURL::URLWithString(url) } {
        unsafe { NSWorkspace::sharedWorkspace().openURL(&url) };
    }
}

// ─── Widget helpers ─────────────────────────────────────────────────────────

fn label(
    mtm: MainThreadMarker,
    text: &str,
    frame: NSRect,
    font: &NSFont,
    color: Option<&NSColor>,
    align: NSTextAlignment,
    wrapping: bool,
) -> Retained<NSTextField> {
    let s = NSString::from_str(text);
    let l = if wrapping {
        unsafe { NSTextField::wrappingLabelWithString(&s, mtm) }
    } else {
        unsafe { NSTextField::labelWithString(&s, mtm) }
    };
    unsafe {
        l.setFrame(frame);
        l.setFont(Some(font));
        if let Some(c) = color {
            l.setTextColor(Some(c));
        }
        l.setAlignment(align);
        l.setSelectable(false);
    }
    l
}

fn symbol_view(
    mtm: MainThreadMarker,
    symbol: &NSString,
    frame: NSRect,
    tint: &NSColor,
) -> Retained<NSImageView> {
    let view = unsafe { NSImageView::new(mtm) };
    unsafe {
        view.setFrame(frame);
        if let Some(img) =
            NSImage::imageWithSystemSymbolName_accessibilityDescription(symbol, None)
        {
            view.setImage(Some(&img));
        }
        view.setContentTintColor(Some(tint));
        view.setImageScaling(NSImageScaling::ScaleProportionallyUpOrDown);
    }
    view
}

fn card(mtm: MainThreadMarker, frame: NSRect) -> Retained<NSBox> {
    let b = unsafe { NSBox::new(mtm) };
    unsafe {
        b.setFrame(frame);
        b.setBoxType(NSBoxType::Custom);
        b.setTitlePosition(NSTitlePosition::NoTitle);
        b.setBorderWidth(0.0);
        b.setCornerRadius(12.0);
        b.setContentViewMargins(NSSize::new(0.0, 0.0));
        b.setFillColor(&NSColor::quaternarySystemFillColor());
    }
    b
}

fn rect(x: f64, y: f64, w: f64, h: f64) -> NSRect {
    NSRect::new(NSPoint::new(x, y), NSSize::new(w, h))
}

// ─── Drag chip ──────────────────────────────────────────────────────────────

pub struct ChipIvars {
    url: Retained<NSURL>,
    icon: Retained<NSImage>,
    icon_frame: Cell<NSRect>,
}

define_class!(
    #[unsafe(super(NSView))]
    #[thread_kind = MainThreadOnly]
    #[name = "JcodeDragChipView"]
    #[ivars = ChipIvars]
    struct DragChipView;

    unsafe impl NSObjectProtocol for DragChipView {}

    impl DragChipView {
        #[unsafe(method(acceptsFirstMouse:))]
        fn accepts_first_mouse(&self, _event: Option<&NSEvent>) -> bool {
            // The panel is non-activating; the very first click must already
            // start the drag or the affordance feels dead.
            true
        }

        #[unsafe(method(mouseDown:))]
        fn mouse_down(&self, event: &NSEvent) {
            let ivars = self.ivars();
            let writer: &ProtocolObject<dyn NSPasteboardWriting> =
                ProtocolObject::from_ref(&*ivars.url);
            let item = unsafe {
                NSDraggingItem::initWithPasteboardWriter(NSDraggingItem::alloc(), writer)
            };
            unsafe {
                item.setDraggingFrame_contents(
                    ivars.icon_frame.get(),
                    Some(&*ivars.icon as &NSImage as &AnyObject),
                );
            }
            let items = NSArray::from_retained_slice(&[item]);
            unsafe {
                self.beginDraggingSessionWithItems_event_source(
                    &items,
                    event,
                    ProtocolObject::from_ref(self),
                );
            }
        }
    }

    unsafe impl NSDraggingSource for DragChipView {
        #[unsafe(method(draggingSession:sourceOperationMaskForDraggingContext:))]
        fn source_operation_mask(
            &self,
            _session: &NSDraggingSession,
            _context: NSDraggingContext,
        ) -> NSDragOperation {
            // System Settings' permission lists accept a generic/copy file
            // drag; nothing is ever moved.
            NSDragOperation::Copy | NSDragOperation::Generic
        }
    }
);

impl DragChipView {
    fn new(
        mtm: MainThreadMarker,
        frame: NSRect,
        url: Retained<NSURL>,
        icon: Retained<NSImage>,
        icon_frame: NSRect,
    ) -> Retained<Self> {
        let this = Self::alloc(mtm).set_ivars(ChipIvars {
            url,
            icon,
            icon_frame: Cell::new(icon_frame),
        });
        unsafe { msg_send![super(this), initWithFrame: frame] }
    }
}

// ─── Controller ─────────────────────────────────────────────────────────────

pub struct ControllerIvars {
    strings: &'static Strings,
    // Never read back, but load-bearing: the strong reference is what keeps
    // the window alive for the controller's lifetime.
    #[allow(dead_code)]
    dialog: Retained<NSWindow>,
    panel: Retained<NSPanel>,
    ax_button: Retained<NSButton>,
    ax_granted: Retained<NSTextField>,
    sr_button: Retained<NSButton>,
    sr_granted: Retained<NSTextField>,
    subtitle: Retained<NSTextField>,
    done_since: Cell<Option<Instant>>,
    demo: bool,
}

define_class!(
    #[unsafe(super(NSObject))]
    #[thread_kind = MainThreadOnly]
    #[name = "JcodeOnboardingController"]
    #[ivars = ControllerIvars]
    struct Controller;

    unsafe impl NSObjectProtocol for Controller {}

    impl Controller {
        #[unsafe(method(allowAccessibility:))]
        fn allow_accessibility(&self, _sender: Option<&AnyObject>) {
            // Fire the real consent alert (this also registers the bundle as
            // a toggled-off row in the list) *and* land the user on the pane:
            // when an earlier denial means macOS won't re-alert, the pane —
            // and the drag bar that will anchor to it — is the only way in.
            tcc::request_accessibility();
            open_pane(tcc::accessibility_pane());
        }

        #[unsafe(method(allowScreenRecording:))]
        fn allow_screen_recording(&self, _sender: Option<&AnyObject>) {
            tcc::request_screen_recording();
            open_pane(tcc::screen_recording_pane());
        }

        #[unsafe(method(tick:))]
        fn tick(&self, _timer: Option<&NSTimer>) {
            self.refresh();
        }
    }

    unsafe impl NSWindowDelegate for Controller {
        #[unsafe(method(windowWillClose:))]
        fn window_will_close(&self, _note: &NSNotification) {
            let mtm = MainThreadMarker::from(self);
            // Closing the dialog is "not now": take the drag bar down too.
            unsafe {
                self.ivars().panel.orderOut(None);
                NSApplication::sharedApplication(mtm).terminate(None);
            }
        }
    }
);

impl Controller {
    fn refresh(&self) {
        let ivars = self.ivars();
        // Demo runs pretend nothing is granted, so the layout under
        // inspection is the one a fresh user sees.
        let ax = tcc::accessibility_granted() && !ivars.demo;
        let sr = tcc::screen_recording_granted() && !ivars.demo;

        ivars.ax_button.setHidden(ax);
        ivars.ax_granted.setHidden(!ax);
        ivars.sr_button.setHidden(sr);
        ivars.sr_granted.setHidden(!sr);

        self.position_drag_bar(ax);

        if ax && sr {
            match ivars.done_since.get() {
                None => {
                    ivars.done_since.set(Some(Instant::now()));
                    unsafe {
                        ivars
                            .subtitle
                            .setStringValue(&NSString::from_str(ivars.strings.all_set));
                        ivars.subtitle.setTextColor(Some(&NSColor::systemGreenColor()));
                    }
                }
                Some(t) if t.elapsed().as_millis() > 1400 => {
                    let mtm = MainThreadMarker::from(self);
                    unsafe { NSApplication::sharedApplication(mtm).terminate(None) };
                }
                Some(_) => {}
            }
        } else {
            // Roll back the green "all set" line if a grant was revoked
            // during the auto-terminate dwell.
            if ivars.done_since.get().is_some() {
                unsafe {
                    ivars
                        .subtitle
                        .setStringValue(&NSString::from_str(ivars.strings.subtitle));
                    ivars
                        .subtitle
                        .setTextColor(Some(&NSColor::secondaryLabelColor()));
                }
            }
            ivars.done_since.set(None);
        }
    }

    /// The "am I visible, and where" decision, re-derived every tick from the
    /// System Settings window's position. Requiring Settings to be the
    /// *frontmost app* — not merely on screen — keeps the floating bar from
    /// hovering over unrelated work while Settings sits buried in a corner.
    fn position_drag_bar(&self, ax_granted: bool) {
        let mtm = MainThreadMarker::from(self);
        let panel = &self.ivars().panel;
        let settings = if ax_granted || !crate::locator::settings_is_frontmost() {
            None
        } else {
            find_settings_window()
        };
        let Some(win) = settings else {
            if panel.isVisible() {
                unsafe { panel.orderOut(None) };
            }
            return;
        };

        // CG global coordinates (top-left origin, y down) → AppKit screen
        // coordinates (bottom-left of the primary display, y up). The primary
        // screen is screens[0] and its AppKit origin is (0,0) by definition.
        let Some(primary) = NSScreen::screens(mtm).firstObject() else {
            return;
        };
        let primary_h = primary.frame().size.height;
        let settings_bottom = primary_h - (win.y + win.h);

        // Hover just inside the Settings window's bottom edge, centered — the
        // spot the eye lands after scrolling the permission list.
        let x = win.x + (win.w - PANEL_W) / 2.0;
        let y = settings_bottom + 20.0;
        unsafe {
            panel.setFrame_display(rect(x, y, PANEL_W, PANEL_H), true);
            if !panel.isVisible() {
                panel.orderFrontRegardless();
            }
        }
    }
}

// ─── Assembly ───────────────────────────────────────────────────────────────

fn build_dialog(
    mtm: MainThreadMarker,
    s: &'static Strings,
    icon: &NSImage,
) -> (
    Retained<NSWindow>,
    Retained<NSButton>,
    Retained<NSTextField>,
    Retained<NSButton>,
    Retained<NSTextField>,
    Retained<NSTextField>,
) {
    let style = NSWindowStyleMask::Titled
        | NSWindowStyleMask::Closable
        | NSWindowStyleMask::FullSizeContentView;
    let window = unsafe {
        NSWindow::initWithContentRect_styleMask_backing_defer(
            NSWindow::alloc(mtm),
            rect(0.0, 0.0, DIALOG_W, DIALOG_H),
            style,
            NSBackingStoreType::Buffered,
            false,
        )
    };
    unsafe {
        // We hold Retained references; the AppKit close-time autorelease would
        // double-free them.
        window.setReleasedWhenClosed(false);
        window.setTitle(&NSString::from_str(s.title));
        window.setTitlebarAppearsTransparent(true);
        window.setTitleVisibility(objc2_app_kit::NSWindowTitleVisibility::Hidden);
        window.setMovableByWindowBackground(true);
        window.center();
    }

    let content = window.contentView().expect("window content view");

    let icon_view = unsafe { NSImageView::new(mtm) };
    unsafe {
        icon_view.setFrame(rect((DIALOG_W - 84.0) / 2.0, DIALOG_H - 52.0 - 84.0, 84.0, 84.0));
        icon_view.setImage(Some(icon));
        icon_view.setImageScaling(NSImageScaling::ScaleProportionallyUpOrDown);
        content.addSubview(&icon_view);
    }

    let title_font = NSFont::boldSystemFontOfSize(24.0);
    let title = label(
        mtm,
        s.title,
        rect(40.0, DIALOG_H - 172.0, DIALOG_W - 80.0, 34.0),
        &title_font,
        None,
        NSTextAlignment::Center,
        false,
    );
    unsafe { content.addSubview(&title) };

    let sub_font = NSFont::systemFontOfSize(13.0);
    let subtitle = label(
        mtm,
        s.subtitle,
        rect(90.0, DIALOG_H - 236.0, DIALOG_W - 180.0, 52.0),
        &sub_font,
        Some(&NSColor::secondaryLabelColor()),
        NSTextAlignment::Center,
        true,
    );
    unsafe { content.addSubview(&subtitle) };

    let (ax_card, ax_button, ax_granted) = permission_card(
        mtm,
        rect(40.0, 140.0, DIALOG_W - 80.0, 96.0),
        ns_string!("accessibility"),
        s.ax_title,
        s.ax_desc,
        s,
        sel!(allowAccessibility:),
    );
    let (sr_card, sr_button, sr_granted) = permission_card(
        mtm,
        rect(40.0, 32.0, DIALOG_W - 80.0, 96.0),
        ns_string!("camera.viewfinder"),
        s.sr_title,
        s.sr_desc,
        s,
        sel!(allowScreenRecording:),
    );
    unsafe {
        content.addSubview(&ax_card);
        content.addSubview(&sr_card);
    }

    (window, ax_button, ax_granted, sr_button, sr_granted, subtitle)
}

fn permission_card(
    mtm: MainThreadMarker,
    frame: NSRect,
    symbol: &NSString,
    title: &str,
    desc: &str,
    s: &Strings,
    action: objc2::runtime::Sel,
) -> (Retained<NSBox>, Retained<NSButton>, Retained<NSTextField>) {
    let w = frame.size.width;
    let card_box = card(mtm, frame);

    let icon = symbol_view(
        mtm,
        symbol,
        rect(26.0, 30.0, 36.0, 36.0),
        &NSColor::systemBlueColor(),
    );

    let title_font = unsafe { NSFont::systemFontOfSize_weight(15.0, objc2_app_kit::NSFontWeightSemibold) };
    let title_label = label(
        mtm,
        title,
        rect(80.0, 52.0, 380.0, 20.0),
        &title_font,
        None,
        NSTextAlignment::Left,
        false,
    );
    let desc_font = NSFont::systemFontOfSize(12.0);
    let desc_label = label(
        mtm,
        desc,
        rect(80.0, 14.0, w - 80.0 - 140.0, 36.0),
        &desc_font,
        Some(&NSColor::secondaryLabelColor()),
        NSTextAlignment::Left,
        true,
    );

    let button = unsafe {
        NSButton::buttonWithTitle_target_action(&NSString::from_str(s.allow), None, Some(action), mtm)
    };
    unsafe { button.setFrame(rect(w - 24.0 - 96.0, 33.0, 96.0, 30.0)) };

    let granted_font = unsafe { NSFont::systemFontOfSize_weight(13.0, objc2_app_kit::NSFontWeightSemibold) };
    let granted = label(
        mtm,
        &format!("✓ {}", s.granted),
        rect(w - 24.0 - 120.0, 38.0, 120.0, 20.0),
        &granted_font,
        Some(&NSColor::systemGreenColor()),
        NSTextAlignment::Right,
        false,
    );
    granted.setHidden(true);

    let content = unsafe { card_box.contentView() }.expect("box content view");
    unsafe {
        content.addSubview(&icon);
        content.addSubview(&title_label);
        content.addSubview(&desc_label);
        content.addSubview(&button);
        content.addSubview(&granted);
    }
    (card_box, button, granted)
}

fn build_drag_bar(
    mtm: MainThreadMarker,
    s: &'static Strings,
    identity: &Identity,
    icon: &NSImage,
) -> Retained<NSPanel> {
    let style = NSWindowStyleMask::Borderless | NSWindowStyleMask::NonactivatingPanel;
    let panel: Retained<NSPanel> = unsafe {
        msg_send![
            NSPanel::alloc(mtm),
            initWithContentRect: rect(0.0, 0.0, PANEL_W, PANEL_H),
            styleMask: style,
            backing: NSBackingStoreType::Buffered,
            defer: false,
        ]
    };
    unsafe {
        panel.setReleasedWhenClosed(false);
        panel.setOpaque(false);
        panel.setBackgroundColor(Some(&NSColor::clearColor()));
        panel.setHasShadow(true);
        panel.setHidesOnDeactivate(false);
        panel.setBecomesKeyOnlyIfNeeded(true);
        panel.setLevel(objc2_app_kit::NSFloatingWindowLevel);
        panel.setCollectionBehavior(
            NSWindowCollectionBehavior::CanJoinAllSpaces
                | NSWindowCollectionBehavior::FullScreenAuxiliary
                | NSWindowCollectionBehavior::Transient,
        );
    }

    let effect = unsafe { NSVisualEffectView::new(mtm) };
    unsafe {
        effect.setFrame(rect(0.0, 0.0, PANEL_W, PANEL_H));
        effect.setMaterial(NSVisualEffectMaterial::Popover);
        effect.setBlendingMode(NSVisualEffectBlendingMode::BehindWindow);
        effect.setState(NSVisualEffectState::Active);
        effect.setWantsLayer(true);
        if let Some(layer) = effect.layer() {
            layer.setCornerRadius(16.0);
            layer.setMasksToBounds(true);
        }
        panel.setContentView(Some(&effect));
    }

    let arrow = symbol_view(
        mtm,
        ns_string!("arrow.up.circle.fill"),
        rect(24.0, PANEL_H - 24.0 - 30.0, 30.0, 30.0),
        &NSColor::systemBlueColor(),
    );
    let hint_font = unsafe { NSFont::systemFontOfSize_weight(13.0, objc2_app_kit::NSFontWeightSemibold) };
    let hint = label(
        mtm,
        s.drag_hint,
        rect(66.0, PANEL_H - 20.0 - 40.0, PANEL_W - 66.0 - 24.0, 38.0),
        &hint_font,
        None,
        NSTextAlignment::Left,
        true,
    );

    // The draggable chip: app icon + name on a filled rounded row.
    let chip_frame = rect(24.0, 16.0, PANEL_W - 48.0, 56.0);
    let icon_frame = rect(12.0, 8.0, 40.0, 40.0);
    let url = unsafe { NSURL::fileURLWithPath(&ns_path(&identity.drag_target)) };
    let chip = DragChipView::new(mtm, chip_frame, url, icon.retain(), icon_frame);

    let chip_bg = card(mtm, rect(0.0, 0.0, chip_frame.size.width, chip_frame.size.height));
    let chip_icon = unsafe { NSImageView::new(mtm) };
    unsafe {
        chip_icon.setFrame(icon_frame);
        chip_icon.setImage(Some(icon));
        chip_icon.setImageScaling(NSImageScaling::ScaleProportionallyUpOrDown);
    }
    let name_font = unsafe { NSFont::systemFontOfSize_weight(14.0, objc2_app_kit::NSFontWeightMedium) };
    let name = label(
        mtm,
        s.app_name,
        rect(64.0, 18.0, chip_frame.size.width - 76.0, 20.0),
        &name_font,
        None,
        NSTextAlignment::Left,
        false,
    );
    unsafe {
        chip.addSubview(&chip_bg);
        chip.addSubview(&chip_icon);
        chip.addSubview(&name);
        effect.addSubview(&arrow);
        effect.addSubview(&hint);
        effect.addSubview(&chip);
    }

    panel
}

/// Render a window's content into a PNG by drawing our own view hierarchy —
/// no Screen Recording involved. Dev tooling for `--demo-shot`.
fn snapshot_window(window: &NSWindow, path: &Path) -> bool {
    let Some(view) = window.contentView() else {
        return false;
    };
    unsafe {
        let bounds = view.bounds();
        let Some(rep) = view.bitmapImageRepForCachingDisplayInRect(bounds) else {
            return false;
        };
        view.cacheDisplayInRect_toBitmapImageRep(bounds, &rep);
        let Some(data) = rep.representationUsingType_properties(
            objc2_app_kit::NSBitmapImageFileType::PNG,
            &objc2_foundation::NSDictionary::new(),
        ) else {
            return false;
        };
        data.writeToFile_atomically(&ns_path(path), true)
    }
}

/// Build everything and run the app. Never returns.
pub fn run(s: &'static Strings, options: RunOptions) -> ! {
    let mtm = MainThreadMarker::new().expect("onboarding UI must start on the main thread");
    let app = NSApplication::sharedApplication(mtm);
    app.setActivationPolicy(NSApplicationActivationPolicy::Accessory);

    let identity = identity();
    let icon = app_icon(&identity);

    let (dialog, ax_button, ax_granted, sr_button, sr_granted, subtitle) =
        build_dialog(mtm, s, &icon);
    let panel = build_drag_bar(mtm, s, &identity, &icon);

    let controller = Controller::alloc(mtm).set_ivars(ControllerIvars {
        strings: s,
        dialog: dialog.clone(),
        panel: panel.clone(),
        ax_button: ax_button.clone(),
        ax_granted,
        sr_button: sr_button.clone(),
        sr_granted,
        subtitle,
        done_since: Cell::new(None),
        demo: options.demo || options.shot_dir.is_some(),
    });
    let controller: Retained<Controller> = unsafe { msg_send![super(controller), init] };

    unsafe {
        // NSControl targets are weak; `controller` stays on this stack frame
        // (below app.run(), which never returns) so the references stay valid.
        ax_button.setTarget(Some(&controller));
        sr_button.setTarget(Some(&controller));
        dialog.setDelegate(Some(ProtocolObject::from_ref(&*controller)));
        let _timer = NSTimer::scheduledTimerWithTimeInterval_target_selector_userInfo_repeats(
            0.5,
            &controller,
            sel!(tick:),
            None,
            true,
        );
    }
    controller.refresh();

    if let Some(dir) = &options.shot_dir {
        let _ = std::fs::create_dir_all(dir);
        let dialog_ok = snapshot_window(&dialog, &dir.join("dialog.png"));
        let panel_ok = snapshot_window(&panel, &dir.join("dragbar.png"));
        eprintln!("demo-shot: dialog={dialog_ok} dragbar={panel_ok}");
        std::process::exit(if dialog_ok && panel_ok { 0 } else { 1 });
    }

    unsafe {
        dialog.makeKeyAndOrderFront(None);
        app.activate();
        // Ask for focus even though our accessory app was launched by a
        // background daemon, not the user. Under macOS 14 cooperative
        // activation this is best-effort (ignoringOtherApps is a no-op now);
        // the floating window level keeps the dialog visible regardless.
        let front = NSRunningApplication::currentApplication();
        let _ = front
            .activateWithOptions(objc2_app_kit::NSApplicationActivationOptions::empty());
    }
    app.run();
    unreachable!("NSApplication.run returned");
}
