// jcode-computerd-capture — short-lived ScreenCaptureKit worker.
//
// ScreenCaptureKit initializes private WindowServer state that can abort the
// process (rather than throw) on some launch paths. Keeping it out of the
// long-lived accessibility daemon means a capture failure cannot poison the AX
// connection or turn every later request into a broken pipe.

import AppKit
import CoreGraphics
import Foundation
import ScreenCaptureKit

struct CaptureMetadata: Codable {
    let x: Double
    let y: Double
    let width: Double
    let height: Double
    let pixel_width: Int
    let pixel_height: Int
}

// ScreenCaptureKit expects an AppKit application context even though this
// helper has no windows of its own.
_ = NSApplication.shared

func flag(_ name: String) -> String? {
    let args = CommandLine.arguments
    for index in 0..<args.count where args[index] == name && index + 1 < args.count {
        return args[index + 1]
    }
    return nil
}

func numberFlag(_ name: String) -> CGFloat? {
    guard let raw = flag(name), let value = Double(raw) else { return nil }
    return CGFloat(value)
}

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data((message + "\n").utf8))
    exit(1)
}

// Permission must be sampled by this executable, not by jcode-computerd. TCC
// authorization is identity-scoped and the ScreenCaptureKit work happens in
// this short-lived worker; asking the AX daemon could otherwise report a grant
// that the process doing the capture does not have.
if CommandLine.arguments.contains("--check-permission") {
    let state = CGPreflightScreenCaptureAccess() ? "granted" : "denied"
    FileHandle.standardOutput.write(Data((state + "\n").utf8))
    exit(0)
}

func requireScreenUnlocked() {
    guard let dict = CGSessionCopyCurrentDictionary() as? [String: Any],
          let onConsole = dict["kCGSSessionOnConsoleKey"] as? Bool,
          let loginDone = dict["kCGSessionLoginDoneKey"] as? Bool,
          onConsole, loginDone else {
        fail("cannot verify an active unlocked console session")
    }
    if let locked = dict["CGSSessionScreenIsLocked"] as? Bool, locked {
        fail("screen is locked")
    }
}

@available(macOS 14.0, *)
func runCapture(pid: pid_t, output: String) {
    requireScreenUnlocked()
    let contentSemaphore = DispatchSemaphore(value: 0)
    var shareableContent: SCShareableContent?
    var contentError: Error?
    SCShareableContent.getExcludingDesktopWindows(true, onScreenWindowsOnly: true) { content, error in
        shareableContent = content
        contentError = error
        contentSemaphore.signal()
    }
    guard contentSemaphore.wait(timeout: .now() + 4) == .success,
          let content = shareableContent else {
        fail("screen capture window lookup failed: \(contentError?.localizedDescription ?? "timeout")")
    }

    let candidates = content.windows.filter { window in
        window.owningApplication?.processID == pid && window.frame.width > 1 && window.frame.height > 1
    }
    let regularWindows = candidates.filter { $0.windowLayer == 0 }
    var narrowed = regularWindows.isEmpty ? candidates : regularWindows
    if let title = flag("--window-title"), !title.isEmpty {
        let titleMatches = candidates.filter { $0.title == title }
        if !titleMatches.isEmpty { narrowed = titleMatches }
    }
    let hintedFrame: CGRect? = {
        guard let x = numberFlag("--window-x"), let y = numberFlag("--window-y"),
              let width = numberFlag("--window-width"), let height = numberFlag("--window-height") else {
            return nil
        }
        return CGRect(x: x, y: y, width: width, height: height)
    }()
    let window: SCWindow?
    if let hint = hintedFrame {
        let ranked = narrowed.map { ($0, frameDistance($0.frame, hint)) }
            .sorted { $0.1 < $1.1 }
        if ranked.count > 1, abs(ranked[0].1 - ranked[1].1) < 0.5 {
            fail("ambiguous focused window for pid \(pid)")
        }
        window = ranked.first?.0
    } else {
        window = narrowed.max(by: {
            ($0.frame.width * $0.frame.height) < ($1.frame.width * $1.frame.height)
        })
    }
    guard let window else {
        fail("no capturable window for pid \(pid)")
    }

    // A display filter still has display-space geometry even when its including
    // list contains one window. The coordinate metadata below is window-space,
    // so use the dedicated single-window filter; otherwise the model can see a
    // scaled/cropped display while being told it maps directly to window bounds.
    let filter = SCContentFilter(desktopIndependentWindow: window)
    let config = SCStreamConfiguration()
    // Vision models do not benefit from an unbounded 5K/6K desktop image, but
    // Base64 expansion and request JSON do. Preserve aspect ratio and cap the
    // long edge before the PNG ever reaches the daemon or Go process.
    let maxDimension: CGFloat = 2048
    let nativeScale = CGFloat(filter.pointPixelScale)
    let longestEdge = Swift.max(window.frame.width, window.frame.height)
    let scale: CGFloat = Swift.min(nativeScale, maxDimension / longestEdge)
    config.width = max(Int((window.frame.width * scale).rounded(.up)), 1)
    config.height = max(Int((window.frame.height * scale).rounded(.up)), 1)
    config.ignoreShadowsSingleWindow = true
    config.showsCursor = false

    let imageSemaphore = DispatchSemaphore(value: 0)
    var image: CGImage?
    var imageError: Error?
    requireScreenUnlocked()
    SCScreenshotManager.captureImage(contentFilter: filter, configuration: config) { captured, error in
        image = captured
        imageError = error
        imageSemaphore.signal()
    }
    guard imageSemaphore.wait(timeout: .now() + 4) == .success,
          let captured = image else {
        fail("screen capture failed: \(imageError?.localizedDescription ?? "timeout")")
    }
    requireScreenUnlocked()

    let bitmap = NSBitmapImageRep(cgImage: captured)
    guard let png = bitmap.representation(using: .png, properties: [:]) else {
        fail("PNG encode failed")
    }
    do {
        try png.write(to: URL(fileURLWithPath: output), options: .atomic)
    } catch {
        fail("write capture: \(error.localizedDescription)")
    }
    let metadata = CaptureMetadata(
        x: Double(window.frame.origin.x), y: Double(window.frame.origin.y),
        width: Double(window.frame.width), height: Double(window.frame.height),
        pixel_width: captured.width, pixel_height: captured.height)
    guard let encoded = try? JSONEncoder().encode(metadata) else { fail("encode capture metadata") }
    FileHandle.standardOutput.write(encoded)
    FileHandle.standardOutput.write(Data("\n".utf8))
}

func frameDistance(_ lhs: CGRect, _ rhs: CGRect) -> CGFloat {
    abs(lhs.origin.x - rhs.origin.x) + abs(lhs.origin.y - rhs.origin.y) +
        abs(lhs.width - rhs.width) + abs(lhs.height - rhs.height)
}

guard let pidText = flag("--pid"), let pid = Int32(pidText), let output = flag("--output") else {
    fail("usage: jcode-computerd-capture --check-permission | --pid <pid> --output <png-path>")
}
if #available(macOS 14.0, *) {
    runCapture(pid: pid, output: output)
} else {
    fail("screenshot requires macOS 14+")
}
