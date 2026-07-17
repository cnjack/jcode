// jcode-computerd — the native computer-use helper daemon (macOS).
//
// It is the platform side of internal/computer's Backend interface: it reads
// accessibility trees, synthesizes input, and captures windows, answering a Go
// client over a unix socket in the wire protocol defined in
// internal/computer/proto.go. It holds no policy — every "may I" is decided in
// Go before a request arrives here (design: the helper is the dumbest process in
// the system).
//
// Build:  make build-computerd   (or: swiftc -O -o jcode-computerd main.swift)
// Run:    jcode-computerd --socket <path> --token-file <path> --shots-dir <path>
//
// See internal-doc/computer-helper-design.md.

import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

// MARK: - Wire protocol (mirrors internal/computer/proto.go)

let apiVersion = "JcodeComputerIPC-1"
let maxFrame = 8 << 20

// error codes, mirroring the Go side 1:1 (proto.go).
enum Code {
    static let senderNotAuthenticated = -10000
    static let appNotAllowed = -10006
    static let accessibilityError = -10008
    static let permissionsNotGranted = -10009
    static let incompatibleVersion = -10013
    static let userIntervened = -10016
    static let ambiguousApp = -10018
    static let screenLocked = -10020
    static let unknown = -10005
}

// An envelope carries one framed message. Payload is kept as raw JSON so each
// handler decodes its own request shape.
struct Envelope: Codable {
    var type: String
    var id: UInt64
    var payload: Data?

    enum CodingKeys: String, CodingKey { case type, id, payload }

    init(type: String, id: UInt64, payload: Data?) {
        self.type = type
        self.id = id
        self.payload = payload
    }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        type = try c.decode(String.self, forKey: .type)
        id = try c.decode(UInt64.self, forKey: .id)
        // payload arrives as embedded JSON; capture it as raw bytes.
        if let raw = try? c.decode(JSONValue.self, forKey: .payload) {
            payload = try? JSONEncoder().encode(raw)
        } else {
            payload = nil
        }
    }

    func encode(to e: Encoder) throws {
        var c = e.container(keyedBy: CodingKeys.self)
        try c.encode(type, forKey: .type)
        try c.encode(id, forKey: .id)
        if let p = payload, let v = try? JSONDecoder().decode(JSONValue.self, from: p) {
            try c.encode(v, forKey: .payload)
        }
    }
}

// JSONValue is a minimal any-JSON box so Envelope can pass a payload through
// without knowing its shape.
indirect enum JSONValue: Codable {
    case null, bool(Bool), integer(Int64), number(Double), string(String)
    case array([JSONValue]), object([String: JSONValue])

    init(from d: Decoder) throws {
        let c = try d.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
        // Preserve integral protocol fields (notably AX refs) as integers.
        // Routing every JSON number through Double loses precision above 2^53
        // and can emit scientific notation that Go refuses for an int64 field.
        else if let i = try? c.decode(Int64.self) { self = .integer(i) }
        else if let n = try? c.decode(Double.self) { self = .number(n) }
        else if let s = try? c.decode(String.self) { self = .string(s) }
        else if let a = try? c.decode([JSONValue].self) { self = .array(a) }
        else if let o = try? c.decode([String: JSONValue].self) { self = .object(o) }
        else { self = .null }
    }
    func encode(to e: Encoder) throws {
        var c = e.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let b): try c.encode(b)
        case .integer(let i): try c.encode(i)
        case .number(let n): try c.encode(n)
        case .string(let s): try c.encode(s)
        case .array(let a): try c.encode(a)
        case .object(let o): try c.encode(o)
        }
    }
}

// Request/response payloads. Keys match the Go structs' JSON tags exactly.
struct PingPayload: Codable {
    var client_api_version: String
    var token: String
}
struct PongPayload: Codable {
    var server_api_version: String
    var platform: String
    var helper_version: String
    // Additive handshake fields. New clients normalize a missing/unknown value
    // from an older daemon to "unknown" rather than assuming the grant exists.
    var accessibility_permission: String
    var screen_recording_permission: String
}
struct AppWire: Codable {
    var bundle_id: String
    var name: String
    var running: Bool
}
struct ListAppsResult: Codable { var apps: [AppWire] }
struct FrontmostResult: Codable { var app: AppWire }
struct AppRequest: Codable { var app: String }
struct TreeRequest: Codable { var app: String; var disable_diff: Bool? }
struct ReadClipboardResult: Codable { var text: String }
struct CaptureResult: Codable {
    var ref: String?
    var png: Data?
    var x: Double?
    var y: Double?
    var width: Double?
    var height: Double?
    var pixel_width: Int?
    var pixel_height: Int?
}
struct CaptureWorkerResult: Codable {
    var x: Double
    var y: Double
    var width: Double
    var height: Double
    var pixel_width: Int
    var pixel_height: Int
}
struct ErrorPayload: Codable { var code: Int; var message: String }

// Node mirrors uitree.Node's JSON (Go exported field names, no tags → PascalCase).
struct NodeState: Codable {
    var Name: String
    var Value: String
}
struct Node: Codable {
    var ID: String
    var Role: String
    var Name: String
    var Value: String
    var States: [NodeState]
    var SemanticID: String
    var Actions: [String]
    var ChildIDs: [String]
    var Ref: Int64
    var Ignored: Bool
}
struct TreeResult: Codable { var nodes: [Node]; var gen: Int }

struct ActionWire: Codable {
    var kind: String
    var bundle_id: String
    var uid: String?
    var ref: Int64?
    var value: String?
    var key: String?
    var text: String?
    var name: String?
    var x: Double?
    var y: Double?
    var to_x: Double?
    var to_y: Double?
    var direction: String?
    var pages: Double?
}
struct PerformRequest: Codable { var action: ActionWire }
struct RequestPermissionsPayload: Codable {
    var accessibility: Bool?
    var screen_recording: Bool?
}

// DaemonError is thrown by handlers and turned into an error frame.
struct DaemonError: Error {
    let code: Int
    let message: String
}

// MARK: - Framing (4-byte little-endian length prefix + JSON)

func readFrame(_ fd: Int32) throws -> Envelope {
    let hdr = try readN(fd, 4)
    let n = UInt32(hdr[0]) | UInt32(hdr[1]) << 8 | UInt32(hdr[2]) << 16 | UInt32(hdr[3]) << 24
    if n > UInt32(maxFrame) {
        throw DaemonError(code: Code.unknown, message: "incoming frame over cap")
    }
    let body = try readN(fd, Int(n))
    return try JSONDecoder().decode(Envelope.self, from: Data(body))
}

func writeFrame(_ fd: Int32, _ env: Envelope) throws {
    let body = try JSONEncoder().encode(env)
    if body.count > maxFrame {
        throw DaemonError(code: Code.unknown, message: "outgoing frame over cap")
    }
    var hdr = [UInt8](repeating: 0, count: 4)
    let n = UInt32(body.count)
    hdr[0] = UInt8(n & 0xff)
    hdr[1] = UInt8((n >> 8) & 0xff)
    hdr[2] = UInt8((n >> 16) & 0xff)
    hdr[3] = UInt8((n >> 24) & 0xff)
    try writeAll(fd, hdr)
    try writeAll(fd, [UInt8](body))
}

func readN(_ fd: Int32, _ n: Int) throws -> [UInt8] {
    var buf = [UInt8](repeating: 0, count: n)
    var got = 0
    while got < n {
        let r = buf.withUnsafeMutableBytes { p in
            read(fd, p.baseAddress!.advanced(by: got), n - got)
        }
        if r <= 0 { throw DaemonError(code: Code.unknown, message: "connection closed") }
        got += r
    }
    return buf
}

func writeAll(_ fd: Int32, _ bytes: [UInt8]) throws {
    var sent = 0
    while sent < bytes.count {
        let w = bytes.withUnsafeBytes { p in
            write(fd, p.baseAddress!.advanced(by: sent), bytes.count - sent)
        }
        if w <= 0 { throw DaemonError(code: Code.unknown, message: "write failed") }
        sent += w
    }
}

func encodePayload<T: Encodable>(_ v: T) -> Data { (try? JSONEncoder().encode(v)) ?? Data("{}".utf8) }
func decodePayload<T: Decodable>(_ t: T.Type, _ data: Data?) throws -> T {
    guard let data = data else { throw DaemonError(code: Code.unknown, message: "missing payload") }
    return try JSONDecoder().decode(t, from: data)
}

// MARK: - Handlers that need no TCC grant (real, run anywhere)

// The model needs to discover an app before it can launch it. Returning only
// runningApplications creates a deadlock for every closed app: it is absent from
// computer_apps, but computer_open requires the bundle id that list was meant to
// discover. Cache the installed catalog and overlay live process state on each
// request. Standard application roots are intentionally bounded; package
// descendants are skipped so this never crawls inside app bundles.
private var installedAppsCache: [String: AppWire]?

func installedApps() -> [String: AppWire] {
    if let cached = installedAppsCache { return cached }

    let fm = FileManager.default
    let home = fm.homeDirectoryForCurrentUser
    let roots = [
        URL(fileURLWithPath: "/Applications", isDirectory: true),
        URL(fileURLWithPath: "/System/Applications", isDirectory: true),
        URL(fileURLWithPath: "/System/Cryptexes/App/System/Applications", isDirectory: true),
        home.appendingPathComponent("Applications", isDirectory: true),
    ]
    var apps: [String: AppWire] = [:]
    for root in roots {
        guard let entries = fm.enumerator(
            at: root,
            includingPropertiesForKeys: nil,
            options: [.skipsHiddenFiles, .skipsPackageDescendants]
        ) else { continue }
        for case let url as URL in entries where url.pathExtension.lowercased() == "app" {
            guard let bundle = Bundle(url: url), let id = bundle.bundleIdentifier, !id.isEmpty else { continue }
            let info = bundle.localizedInfoDictionary ?? bundle.infoDictionary ?? [:]
            let name = (info["CFBundleDisplayName"] as? String)
                ?? (info["CFBundleName"] as? String)
                ?? url.deletingPathExtension().lastPathComponent
            apps[id] = AppWire(bundle_id: id, name: name, running: false)
        }
    }
    installedAppsCache = apps
    return apps
}

func handleListApps() -> ListAppsResult {
    var byBundle = installedApps()
    for app in NSWorkspace.shared.runningApplications {
        guard let bundle = app.bundleIdentifier else { continue }
        // Only regular apps have a UI worth automating; skip agents/daemons.
        guard app.activationPolicy == .regular else { continue }
        byBundle[bundle] = AppWire(bundle_id: bundle, name: app.localizedName ?? bundle, running: true)
    }
    let apps = byBundle.values.sorted {
        if $0.running != $1.running { return $0.running && !$1.running }
        return $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
    }
    return ListAppsResult(apps: apps)
}

func handleFrontmost() throws -> FrontmostResult {
    guard let app = NSWorkspace.shared.frontmostApplication, let bundle = app.bundleIdentifier else {
        throw DaemonError(code: Code.unknown, message: "no frontmost application")
    }
    return FrontmostResult(app: AppWire(bundle_id: bundle, name: app.localizedName ?? bundle, running: true))
}

func handleLaunch(_ req: AppRequest) throws {
    guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: req.app) else {
        throw DaemonError(code: Code.unknown, message: "app not found: \(req.app)")
    }
    let cfg = NSWorkspace.OpenConfiguration()
    cfg.activates = true
    let sem = DispatchSemaphore(value: 0)
    var launchErr: Error?
    NSWorkspace.shared.openApplication(at: url, configuration: cfg) { _, err in
        launchErr = err
        sem.signal()
    }
    guard sem.wait(timeout: .now() + 10) == .success else {
        throw DaemonError(code: Code.unknown, message: "launch timed out after 10 seconds")
    }
    if let e = launchErr { throw DaemonError(code: Code.unknown, message: e.localizedDescription) }
}

func handleReadClipboard() -> ReadClipboardResult {
    ReadClipboardResult(text: NSPasteboard.general.string(forType: .string) ?? "")
}

// MARK: - TCC consent prompts (point-of-need permission requests)
//
// A grant that can only be discovered by digging through System Settings is a
// grant users never find (the settings page's "which gate is shut" story only
// helps once the user opens it). So the daemon can surface the real macOS
// consent prompt itself: explicitly via the request_permissions RPC (Settings →
// Computer Use → Request permission and /computer grant both ride it), and
// automatically the first time a request actually fails for lack of the grant.

// requestAccessibilityPermission triggers the system "jcode-computerd would
// like to control this computer" alert when the grant is missing. The alert is
// asynchronous — this returns the current state immediately, never blocks on
// the user's answer, and is idempotent (macOS will not stack duplicate alerts).
@discardableResult
func requestAccessibilityPermission() -> Bool {
    let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
    return AXIsProcessTrustedWithOptions(options)
}

// The automatic prompt fires at most once per daemon launch per service, so an
// agent loop that keeps hitting the missing grant cannot stack alerts. The
// explicit RPC path above bypasses this gate: a user clicking "Request
// permission" always gets a fresh prompt.
var didAutoPromptAccessibility = false
var didAutoPromptScreenRecording = false

// MARK: - Onboarding UI (the branded permission ceremony)
//
// When the helpers run from inside jcode-computerd.app, permission requests
// surface through the bundled onboarding window (jcode-computerd-onboarding,
// Rust/AppKit): the "Enable jcode Computer Use" dialog with per-grant Allow
// buttons, plus a drag-into-Settings affordance that anchors itself to the
// System Settings window. The UI executable lives in the same bundle, so
// every TCC call it makes is attributed to the same "jcode Computer Use"
// identity as this daemon and the capture worker — one identity, one row to
// authorize. Bare-binary runs (make build-computerd, unit tests) have no
// bundle identity worth priming, so they keep the direct TCC prompts below.

// The .app bundle is the unit of TCC identity. Outside it (bare dev binaries)
// the onboarding window would prime a throwaway per-binary identity, so it
// stays off and callers fall back to the bare prompts. Identity is verified
// against the bundle's Info.plist, not just the path shape — bare helpers
// that happen to live in some other app's Contents/MacOS (the Tauri desktop
// app ships sidecars that way) must not count.
let helperBundleIdentifier = "com.cnjack.jcode.computerd"

func helperBundleRoot() -> URL? {
    var dir = URL(fileURLWithPath: CommandLine.arguments[0]).standardizedFileURL
        .deletingLastPathComponent()
    guard dir.lastPathComponent == "MacOS" else { return nil }
    dir.deleteLastPathComponent()
    guard dir.lastPathComponent == "Contents" else { return nil }
    dir.deleteLastPathComponent()
    guard dir.pathExtension == "app",
          Bundle(url: dir)?.bundleIdentifier == helperBundleIdentifier else { return nil }
    return dir
}

func isRunningFromHelperBundle() -> Bool { helperBundleRoot() != nil }

func onboardingHelperURL() -> URL? {
    if let override = ProcessInfo.processInfo.environment["JCODE_COMPUTERD_ONBOARDING"],
       FileManager.default.isExecutableFile(atPath: override) {
        let path = URL(fileURLWithPath: override).standardizedFileURL.path
        if let root = helperBundleRoot(), path.hasPrefix(root.path + "/") {
            return URL(fileURLWithPath: path)
        }
        // An out-of-bundle UI would prime its own throwaway TCC identity
        // while the ceremony claims success for "jcode Computer Use" —
        // refuse it (callers fall back to bare prompts) rather than mislead.
        FileHandle.standardError.write(
            "jcode-computerd: ignoring JCODE_COMPUTERD_ONBOARDING outside the helper bundle: \(path)\n"
                .data(using: .utf8)!)
    }
    let daemon = URL(fileURLWithPath: CommandLine.arguments[0]).standardizedFileURL
    let name = daemon.lastPathComponent
    let prefix = "jcode-computerd"
    guard name.hasPrefix(prefix) else { return nil }
    let suffix = String(name.dropFirst(prefix.count))
    let sibling = daemon.deletingLastPathComponent()
        .appendingPathComponent(prefix + "-onboarding" + suffix)
    if FileManager.default.isExecutableFile(atPath: sibling.path) { return sibling }
    let unsuffixed = daemon.deletingLastPathComponent()
        .appendingPathComponent(prefix + "-onboarding")
    return FileManager.default.isExecutableFile(atPath: unsuffixed.path) ? unsuffixed : nil
}

// MARK: - Self-responsibility (TCC attribution for the daemon itself)
//
// jcode spawns this daemon with a plain fork/exec, so by default it inherits
// jcode's *responsible process* — Terminal/iTerm for CLI runs, the desktop
// app for Tauri — and every AXIsProcessTrusted/AX call in this process would
// key on THAT identity. The onboarding ceremony below obtains the grant for
// the bundle identity ("jcode Computer Use"); without this step the row the
// user just enabled would never satisfy requireAccessibilityTrusted. So when
// running from the helper bundle, re-exec once through the same disclaim SPI
// the workers use, making the daemon self-responsible (= the bundle). The
// original process lingers only as a signal-forwarding supervisor so the Go
// parent's process handle — and its kill on failed dials — still reaches the
// real daemon.

var reexecChildPID: pid_t = 0

func maybeReexecSelfResponsible() {
    guard isRunningFromHelperBundle(),
          ProcessInfo.processInfo.environment["JCODE_COMPUTERD_SELF_DISCLAIMED"] != "1"
    else { return }

    var attr: posix_spawnattr_t? = nil
    // Every failure path degrades to running with inherited responsibility —
    // worse attribution, but a working daemon beats a dead one.
    guard posix_spawnattr_init(&attr) == 0 else { return }
    defer { posix_spawnattr_destroy(&attr) }
    guard responsibility_spawnattrs_setdisclaim(&attr, 1) == 0 else { return }

    var argv: [UnsafeMutablePointer<CChar>?] = CommandLine.arguments.map { strdup($0) } + [nil]
    defer { for a in argv { free(a) } }
    var env = ProcessInfo.processInfo.environment
    env["JCODE_COMPUTERD_SELF_DISCLAIMED"] = "1"
    var envp: [UnsafeMutablePointer<CChar>?] = env.map { strdup("\($0.key)=\($0.value)") } + [nil]
    defer { for e in envp { free(e) } }

    var pid = pid_t()
    let exe = URL(fileURLWithPath: CommandLine.arguments[0]).standardizedFileURL.path
    guard posix_spawn(&pid, exe, nil, &attr, &argv, &envp) == 0 else { return }

    reexecChildPID = pid
    signal(SIGTERM) { _ in kill(reexecChildPID, SIGTERM) }
    signal(SIGINT) { _ in kill(reexecChildPID, SIGINT) }
    var status: Int32 = 0
    while waitpid(pid, &status, 0) == -1 && errno == EINTR {}
    if (status & 0x7f) == 0 { exit((status >> 8) & 0xff) }
    exit(128 + (status & 0x7f))
}

// Reaped on demand (pollStatus in isRunning) rather than in a background
// thread: RPC handling is single-threaded, and on-demand reaping keeps
// isRunning race-free. A finished UI lingers as a zombie only until the next
// surface call or daemon exit.
var onboardingProcess: WorkerProcess?

/// Opens the onboarding window, or leaves the already-open one in place.
/// Returns false when the UI is unavailable (bare binaries, missing
/// executable, spawn failure) — callers then fall back to bare TCC prompts.
@discardableResult
func surfaceOnboardingUI() -> Bool {
    guard isRunningFromHelperBundle(), let ui = onboardingHelperURL() else { return false }
    if let running = onboardingProcess, running.isRunning {
        // An explicit re-request must not be a silent no-op: the accessory-
        // policy window has no Dock icon to find, so re-front it ourselves.
        // Best-effort under macOS 14 cooperative activation; the window's
        // floating level keeps it visible even when activation is denied.
        NSRunningApplication(processIdentifier: running.pid)?.activate(options: [])
        return true
    }
    do {
        // Disclaimed for the same reason as the capture worker: the UI's TCC
        // calls must be attributed to the helper bundle, not to whichever
        // process launched jcode.
        onboardingProcess = try spawnDisclaimedWorker(
            executable: ui, arguments: [], stdout: Pipe(), stderr: Pipe())
        return true
    } catch {
        return false
    }
}

func requireAccessibilityTrusted() throws {
    if AXIsProcessTrusted() { return }
    if !didAutoPromptAccessibility {
        didAutoPromptAccessibility = true
        if !surfaceOnboardingUI() { _ = requestAccessibilityPermission() }
    }
    throw DaemonError(
        code: Code.permissionsNotGranted,
        message: "Accessibility permission not granted for jcode-computerd. A permission window or macOS consent prompt was shown — approve it, or enable jcode Computer Use under System Settings › Privacy & Security › Accessibility. The user can re-open the prompt from jcode Settings → Computer Use → Request permission, or with /computer grant.")
}

func handleRequestPermissions(_ req: RequestPermissionsPayload) -> PongPayload {
    // The onboarding window covers both grants at once; when it cannot be
    // shown, fire exactly the bare prompts the client asked for.
    if !surfaceOnboardingUI() {
        if req.accessibility == true { _ = requestAccessibilityPermission() }
        if req.screen_recording == true { _ = requestCaptureWorkerPermission() }
    }
    return currentPong()
}

// MARK: - Accessibility (needs the Accessibility TCC grant)

func runningApp(_ bundleID: String) throws -> NSRunningApplication {
    let matches = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID)
    if matches.isEmpty { throw DaemonError(code: Code.appNotAllowed, message: bundleID) }
    if matches.count == 1 { return matches[0] }
    if let front = NSWorkspace.shared.frontmostApplication,
       front.bundleIdentifier == bundleID,
       let exact = matches.first(where: { $0.processIdentifier == front.processIdentifier }) {
        return exact
    }
    throw DaemonError(code: Code.ambiguousApp,
                      message: "multiple processes are running for \(bundleID); focus the intended one and retry")
}

func axValue(_ el: AXUIElement, _ attr: String) -> CFTypeRef? {
    if currentAXFatalError != nil { return nil }
    var value: CFTypeRef?
    let result = AXUIElementCopyAttributeValue(el, attr as CFString, &value)
    switch result {
    case .success:
        return value
    case .attributeUnsupported, .noValue:
        return nil
    case .apiDisabled:
        currentAXFatalError = DaemonError(
            code: Code.permissionsNotGranted, message: "Accessibility API is disabled")
    case .cannotComplete:
        currentAXFatalError = DaemonError(
            code: Code.accessibilityError, message: "target app did not answer Accessibility within the timeout")
    case .invalidUIElement:
        currentAXFatalError = DaemonError(
            code: Code.accessibilityError, message: "Accessibility element became invalid; take a fresh snapshot")
    default:
        currentAXFatalError = DaemonError(
            code: Code.accessibilityError, message: "Accessibility read failed: \(result.rawValue)")
    }
    return nil
}

var currentAXFatalError: DaemonError?

func axString(_ el: AXUIElement, _ attr: String) -> String {
    guard let value = axValue(el, attr) else { return "" }
    if let string = value as? String { return string }
    if let number = value as? NSNumber { return number.stringValue }
    return ""
}

func axBool(_ el: AXUIElement, _ attr: String, default fallback: Bool = false) -> Bool {
    guard let value = axValue(el, attr) else { return fallback }
    if let bool = value as? Bool { return bool }
    if let number = value as? NSNumber { return number.boolValue }
    return fallback
}

func axElement(_ el: AXUIElement, _ attr: String) -> AXUIElement? {
    guard let value = axValue(el, attr), CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
    return unsafeBitCast(value, to: AXUIElement.self)
}

func axChildren(_ el: AXUIElement) -> [AXUIElement] {
    axValue(el, kAXChildrenAttribute) as? [AXUIElement] ?? []
}

func axSecondaryActions(_ el: AXUIElement) -> [String] {
    if currentAXFatalError != nil { return [] }
    var values: CFArray?
    let result = AXUIElementCopyActionNames(el, &values)
    guard result == .success else {
        if result == .cannotComplete {
            currentAXFatalError = DaemonError(
                code: Code.accessibilityError,
                message: "target app did not answer Accessibility within the timeout")
        } else if result != .actionUnsupported && result != .noValue {
            currentAXFatalError = DaemonError(
                code: Code.accessibilityError,
                message: "Accessibility action lookup failed: \(result.rawValue)")
        }
        return []
    }
    guard let actions = values as? [String] else { return [] }
    // AXPress is already the primary click verb. Showing it on every button
    // wastes tokens; secondary actions are the names computer_act(menu) needs.
    return actions.filter { $0 != (kAXPressAction as String) }
}

// AX uses a platform vocabulary (AXButton, AXWindow, ...), while uitree uses
// the browser-style roles the agent already knows (button, window, ...). The Go
// renderer intentionally only emits normalized roles; forwarding raw AX roles
// made a healthy Calculator tree render as "no interactive elements".
func normalizeRole(_ role: String) -> String {
    switch role {
    case "AXButton": return "button"
    case "AXLink": return "link"
    case "AXTextField": return "textbox"
    case "AXTextArea": return "textarea"
    case "AXCheckBox": return "checkbox"
    case "AXRadioButton": return "radio"
    case "AXPopUpButton": return "popupbutton"
    case "AXMenuButton": return "menubutton"
    case "AXMenuItem": return "menuitem"
    case "AXComboBox": return "combobox"
    case "AXList": return "listbox"
    case "AXRow": return "row"
    case "AXCell": return "cell"
    case "AXSlider": return "slider"
    case "AXIncrementor": return "incrementor"
    case "AXDisclosureTriangle": return "disclosuretriangle"
    case "AXColorWell": return "colorwell"
    case "AXWindow": return "window"
    case "AXSheet": return "sheet"
    case "AXGroup", "AXSplitGroup", "AXScrollArea": return "group"
    case "AXToolbar": return "toolbar"
    case "AXStaticText": return "statictext"
    case "AXImage": return "image"
    case "AXHeading": return "heading"
    default: return role
    }
}

func elementName(_ el: AXUIElement) -> String {
    for attr in [kAXTitleAttribute, kAXDescriptionAttribute, kAXHelpAttribute, kAXIdentifierAttribute] {
        let value = axString(el, attr).trimmingCharacters(in: .whitespacesAndNewlines)
        if !value.isEmpty { return value }
    }
    return ""
}

func accessibilityRoot(_ app: NSRunningApplication) -> AXUIElement {
    let root = AXUIElementCreateApplication(app.processIdentifier)
    if let focused = axElement(root, kAXFocusedWindowAttribute) { return focused }
    if let main = axElement(root, kAXMainWindowAttribute) { return main }
    return root
}

// ElementKey makes an AXUIElement hashable via CFEqual/CFHash so it can key the
// ref table. Two AXUIElements pointing at the same UI element compare equal, so
// the same element gets the same Ref across snapshots.
struct ElementKey: Hashable {
    let element: AXUIElement
    static func == (l: ElementKey, r: ElementKey) -> Bool { CFEqual(l.element, r.element) }
    func hash(into h: inout Hasher) { h.combine(CFHash(element)) }
}

// ElementRegistry gives each AXUIElement a Ref that is STABLE for the session's
// lifetime — the same element seen in two snapshots gets the same Ref, and an
// element that disappears keeps its (now-dead) Ref reserved, never reissued.
//
// This is load-bearing, and the naive version (a fresh counter per snapshot)
// gets it wrong: uitree above the line uses Ref as element identity, so a Ref
// that changed for the same button would make every uid churn on every snapshot,
// break the diff, and defeat stale-uid detection. Persisting element→Ref is what
// lets uitree's "same element keeps its uid, departed element's uid retires"
// property actually hold (design §1.1, §9.1).
final class ElementRegistry {
    let processIdentifier: pid_t
    let rootWindow: AXUIElement
    private var byElement: [ElementKey: Int64] = [:]
    private var byRef: [Int64: AXUIElement] = [:]
    // A new daemon must not accidentally reuse an old daemon's small ref values:
    // an existing Go Session may reconnect after a crash. A random per-registry
    // base makes an old uid fail closed until a fresh snapshot is taken.
    private var nextRef = Int64.random(in: 1_000_000...(Int64.max / 4))

    init(processIdentifier: pid_t, rootWindow: AXUIElement) {
        self.processIdentifier = processIdentifier
        self.rootWindow = rootWindow
    }

    func matches(processIdentifier: pid_t, rootWindow: AXUIElement) -> Bool {
        self.processIdentifier == processIdentifier && CFEqual(self.rootWindow, rootWindow)
    }

    func refFor(_ el: AXUIElement) -> Int64 {
        let key = ElementKey(element: el)
        if let r = byElement[key] { return r }
        nextRef += 1
        byElement[key] = nextRef
        byRef[nextRef] = el
        return nextRef
    }

    func element(_ ref: Int64) -> AXUIElement? { byRef[ref] }

    func retain(activeRefs: Set<Int64>) {
        byRef = byRef.filter { activeRefs.contains($0.key) }
        byElement = byElement.filter { activeRefs.contains($0.value) }
    }
}

// TreeBuilder walks an app's AX tree into flat Nodes, assigning each actionable
// element a session-stable Ref from the registry. AX trees can contain cycles
// and enormous virtualized subtrees, so traversal is explicitly bounded.
final class TreeBuilder {
    private(set) var nodes: [Node] = []
    private(set) var activeRefs: Set<Int64> = []
    private let registry: ElementRegistry
    private var nextID = 0
    private var seen: Set<ElementKey> = []
    private let maxDepth = 12
    private let maxNodes = 400
    private let maxChildren = 120

    init(registry: ElementRegistry) { self.registry = registry }

    func build(_ root: AXUIElement) { _ = walk(root, depth: 0) }

    private func walk(_ el: AXUIElement, depth: Int) -> String? {
        guard depth <= maxDepth, nextID < maxNodes else { return nil }
        let key = ElementKey(element: el)
        guard !seen.contains(key) else { return nil }
        seen.insert(key)

        nextID += 1
        let id = String(nextID)

        let role = normalizeRole(axString(el, kAXRoleAttribute))
        let name = elementName(el)
        let value = axString(el, kAXValueAttribute)
        let semanticID = axString(el, kAXIdentifierAttribute)
        let actions = axSecondaryActions(el)
        let focused = axBool(el, kAXFocusedAttribute)
        let enabled = axBool(el, kAXEnabledAttribute, default: true)
        let selected = axBool(el, kAXSelectedAttribute)
        let expanded = axBool(el, kAXExpandedAttribute)

        var ref: Int64 = 0
        // Only actionable elements get a ref (mirrors uitree: a node the backend
        // can't resolve should not get a uid).
        if isActionable(role) {
            ref = registry.refFor(el)
            activeRefs.insert(ref)
        }

        var childIDs: [String] = []
        for child in axChildren(el).prefix(maxChildren) {
            if let childID = walk(child, depth: depth + 1) { childIDs.append(childID) }
        }

        var states: [NodeState] = []
        if focused { states.append(NodeState(Name: "focused", Value: "true")) }
        if !enabled { states.append(NodeState(Name: "disabled", Value: "true")) }
        if selected { states.append(NodeState(Name: "selected", Value: "true")) }
        if expanded { states.append(NodeState(Name: "expanded", Value: "true")) }
        if role == "checkbox" || role == "radio" {
            states.append(NodeState(Name: "checked", Value: axBool(el, kAXValueAttribute) ? "true" : "false"))
        }

        nodes.append(Node(
            ID: id, Role: role, Name: name, Value: value, States: states,
            SemanticID: semanticID, Actions: actions,
            ChildIDs: childIDs, Ref: ref, Ignored: false))
        return id
    }

    private func isActionable(_ role: String) -> Bool {
        switch role {
        case "button", "link", "textbox", "textarea", "checkbox", "radio",
             "popupbutton", "menubutton", "menuitem", "combobox", "listbox",
             "slider", "incrementor", "disclosuretriangle", "row", "colorwell":
            return true
        default:
            return false
        }
    }
}

func handleTree(_ req: TreeRequest, _ session: Session) throws -> TreeResult {
    try requireAccessibilityTrusted()
    try checkScreenUnlocked()
    let app = try runningApp(req.app)
    let root = accessibilityRoot(app)
    let registry = session.registry(
        for: req.app, processIdentifier: app.processIdentifier, rootWindow: root)
    session.bindWindow(req.app, processIdentifier: app.processIdentifier, rootWindow: root)
    let builder = TreeBuilder(registry: registry)
    builder.build(root)
    if let error = currentAXFatalError { throw error }
    // Retire stale AXUIElement objects after a successful snapshot. nextRef is
    // monotonic, so removed refs are never reused, while dynamic apps cannot
    // grow the daemon heap without bound across a long-lived connection.
    registry.retain(activeRefs: builder.activeRefs)
    session.gen += 1
    return TreeResult(nodes: builder.nodes, gen: session.gen)
}

// MARK: - Perform (input synthesis + AX actions)

func handlePerform(_ req: PerformRequest, _ session: Session) throws {
    try requireAccessibilityTrusted()
    try checkScreenUnlocked()
    let a = req.action
    // The Go tier gate and this dispatch are separate RPCs. Re-check in the
    // process that actually posts input so a focus switch in between cannot
    // route a key/click into an ungranted app.
    let front = try requireFrontmost(a.bundle_id)
    let currentRoot = accessibilityRoot(front)
    guard session.matchesBoundWindow(
        a.bundle_id, processIdentifier: front.processIdentifier, rootWindow: currentRoot) else {
        throw DaemonError(code: Code.userIntervened,
                          message: "process or focused window changed since the last snapshot/screenshot")
    }
    switch a.kind {
    case "set_value":
        guard let ref = a.ref, let el = session.boundRegistry(for: a.bundle_id)?.element(ref) else {
            throw DaemonError(code: Code.accessibilityError, message: "no live element for ref")
        }
        try requireFrontmost(a.bundle_id)
        let r = AXUIElementSetAttributeValue(el, kAXValueAttribute as CFString, (a.value ?? "") as CFString)
        if r != .success { throw mutationAXError("set_value", r) }
    case "menu":
        guard let ref = a.ref, let el = session.boundRegistry(for: a.bundle_id)?.element(ref), let name = a.name else {
            throw DaemonError(code: Code.accessibilityError, message: "menu needs a live element and an action name")
        }
        try requireFrontmost(a.bundle_id)
        let r = AXUIElementPerformAction(el, name as CFString)
        if r != .success { throw mutationAXError("action \(name)", r) }
    case "click", "dblclick", "rclick":
        try performClick(a, session)
    case "hover":
        let point = try actionPoint(a, session)
        guard let event = CGEvent(mouseEventSource: nil, mouseType: .mouseMoved,
                                  mouseCursorPosition: point, mouseButton: .left) else {
            throw DaemonError(code: Code.accessibilityError, message: "cannot create hover event")
        }
        try requireFrontmost(a.bundle_id)
        event.post(tap: .cghidEventTap)
    case "drag":
        try synthDrag(a, session)
    case "type":
        try focusReferencedElement(a, session)
        try synthType(a.text ?? "", bundleID: a.bundle_id)
    case "press":
        try synthKey(a.key ?? "", bundleID: a.bundle_id)
    case "scroll":
        try synthScroll(a, session)
    case "select_text":
        guard let ref = a.ref, let el = session.boundRegistry(for: a.bundle_id)?.element(ref) else {
            throw DaemonError(code: Code.accessibilityError, message: "select_text needs a live element")
        }
        let text = a.value ?? ""
        try requireFrontmost(a.bundle_id)
        var result = AXUIElementSetAttributeValue(el, kAXSelectedTextAttribute as CFString, text as CFString)
        if result == .cannotComplete { throw mutationAXError("select_text", result) }
        if result != .success {
            // Some native selectors expose their selected option as AXValue
            // rather than AXSelectedText. Try that contract before failing.
            result = AXUIElementSetAttributeValue(el, kAXValueAttribute as CFString, text as CFString)
        }
        if result != .success {
            throw mutationAXError("select_text", result)
        }
    default:
        throw DaemonError(code: Code.unknown, message: "unsupported action: \(a.kind)")
    }

    // Auto-wait: give the UI a moment to settle before returning, so the next
    // snapshot the agent takes reflects this action's effect rather than racing
    // it (design §7: retry-until-settled, ~1s baseline). A fixed short settle is
    // the pragmatic form; a fuller implementation would poll the tree for
    // stability and extend under a loading indicator. Reads (list/tree/capture)
    // do not settle — only actions that mutate the UI.
    settleUI()
}

// settleUI blocks briefly to let synthesized input propagate and the UI redraw.
// Kept small (actions are frequent); the parent design's up-to-5s extension
// under a loading indicator is phase-2 polish.
func settleUI() {
    Thread.sleep(forTimeInterval: 0.6)
}

// checkScreenUnlocked refuses to act while the screen is locked. An agent
// driving a machine its owner believes is secured is not a feature (design §8);
// this is a fail-safe (stop), enforced here because only the daemon can see the
// live session state.
func checkScreenUnlocked() throws {
    guard let dict = CGSessionCopyCurrentDictionary() as? [String: Any],
          let onConsole = dict["kCGSSessionOnConsoleKey"] as? Bool,
          let loginDone = dict["kCGSessionLoginDoneKey"] as? Bool,
          onConsole, loginDone else {
        throw DaemonError(code: Code.screenLocked,
                          message: "cannot verify an active unlocked console session")
    }
    if let locked = dict["CGSSessionScreenIsLocked"] as? Bool, locked {
        throw DaemonError(code: Code.screenLocked, message: "the screen is locked")
    }
}

func mutationAXError(_ operation: String, _ result: AXError) -> DaemonError {
    if result == .cannotComplete {
        return DaemonError(
            code: Code.accessibilityError,
            message: "\(operation) timed out; the outcome is unknown — inspect fresh UI state before retrying")
    }
    return DaemonError(
        code: Code.accessibilityError, message: "\(operation) failed: \(result.rawValue)")
}

@discardableResult
func requireFrontmost(_ bundleID: String) throws -> NSRunningApplication {
    guard let front = NSWorkspace.shared.frontmostApplication,
          front.bundleIdentifier == bundleID else {
        throw DaemonError(code: Code.userIntervened,
                          message: "frontmost app changed before input; expected \(bundleID)")
    }
    return front
}

func focusedWindowFrame(_ bundleID: String) throws -> CGRect {
    let app = try runningApp(bundleID)
    guard let frame = elementFrame(accessibilityRoot(app)), frame.width > 1, frame.height > 1 else {
        throw DaemonError(code: Code.accessibilityError,
                          message: "cannot resolve focused window bounds for \(bundleID)")
    }
    return frame
}

func requirePointInFocusedWindow(_ point: CGPoint, bundleID: String) throws {
    let frame = try focusedWindowFrame(bundleID)
    guard frame.contains(point) else {
        throw DaemonError(code: Code.accessibilityError,
                          message: "coordinate (\(point.x),\(point.y)) is outside the focused \(bundleID) window")
    }
}

func focusReferencedElement(_ a: ActionWire, _ session: Session) throws {
    guard let ref = a.ref else { return }
    guard let el = session.boundRegistry(for: a.bundle_id)?.element(ref) else {
        throw DaemonError(code: Code.accessibilityError, message: "no live element for ref")
    }
    // The handler-level check and the actual AX mutation are separated by ref
    // lookup. Re-check at the mutation boundary so a user focus switch cannot
    // make us focus a control in an app that is no longer frontmost.
    try requireFrontmost(a.bundle_id)
    let result = AXUIElementSetAttributeValue(el, kAXFocusedAttribute as CFString, kCFBooleanTrue)
    // Not every actionable element exposes AXFocused as settable. Typing into a
    // ref that cannot be focused is unsafe, so fail instead of sending text to
    // whichever control happened to be active.
    if result != .success {
        throw mutationAXError("focus referenced element", result)
    }
}

func elementCenter(_ el: AXUIElement) -> CGPoint? {
    guard let frame = elementFrame(el) else { return nil }
    return CGPoint(x: frame.origin.x + frame.size.width / 2,
                   y: frame.origin.y + frame.size.height / 2)
}

func elementFrame(_ el: AXUIElement) -> CGRect? {
    guard let positionValue = axValue(el, kAXPositionAttribute),
          CFGetTypeID(positionValue) == AXValueGetTypeID(),
          let sizeValue = axValue(el, kAXSizeAttribute),
          CFGetTypeID(sizeValue) == AXValueGetTypeID() else { return nil }
    let axPosition = positionValue as! AXValue
    let axSize = sizeValue as! AXValue
    var position = CGPoint.zero
    var size = CGSize.zero
    guard AXValueGetValue(axPosition, .cgPoint, &position),
          AXValueGetValue(axSize, .cgSize, &size) else { return nil }
    return CGRect(origin: position, size: size)
}

func actionPoint(_ a: ActionWire, _ session: Session) throws -> CGPoint {
    if let ref = a.ref {
        guard let el = session.boundRegistry(for: a.bundle_id)?.element(ref) else {
            throw DaemonError(code: Code.accessibilityError, message: "no live element for ref")
        }
        guard let point = elementCenter(el) else {
            throw DaemonError(code: Code.accessibilityError, message: "referenced element has no usable bounds")
        }
        try requirePointInFocusedWindow(point, bundleID: a.bundle_id)
        return point
    }
    guard let x = a.x, let y = a.y else {
        throw DaemonError(code: Code.accessibilityError, message: "action needs a live ref or explicit x/y coordinates")
    }
    let point = CGPoint(x: x, y: y)
    try requirePointInFocusedWindow(point, bundleID: a.bundle_id)
    return point
}

func performClick(_ a: ActionWire, _ session: Session) throws {
    if let ref = a.ref {
        guard let el = session.boundRegistry(for: a.bundle_id)?.element(ref) else {
            throw DaemonError(code: Code.accessibilityError, message: "no live element for ref")
        }
        if a.kind == "click" {
            try requireFrontmost(a.bundle_id)
            let result = AXUIElementPerformAction(el, kAXPressAction as CFString)
            if result == .success { return }
            if result == .cannotComplete { throw mutationAXError("click", result) }
        }
        if a.kind == "rclick" {
            try requireFrontmost(a.bundle_id)
            let result = AXUIElementPerformAction(el, kAXShowMenuAction as CFString)
            if result == .success { return }
            if result == .cannotComplete { throw mutationAXError("right click", result) }
        }
    }
    try synthClick(kind: a.kind, at: actionPoint(a, session), bundleID: a.bundle_id)
}

// synthClick posts a mouse click at a resolved point. Input is delivered to
// whatever holds focus — the coordinate carries no target identity — which is
// exactly why the Go side re-checks the frontmost app before every action.
func synthClick(kind: String, at pt: CGPoint, bundleID: String) throws {
    let (down, up, button): (CGEventType, CGEventType, CGMouseButton)
    if kind == "rclick" {
        (down, up, button) = (.rightMouseDown, .rightMouseUp, .right)
    } else {
        (down, up, button) = (.leftMouseDown, .leftMouseUp, .left)
    }
    let clicks = kind == "dblclick" ? 2 : 1
    for i in 1...clicks {
        try requireFrontmost(bundleID)
        if let d = CGEvent(mouseEventSource: nil, mouseType: down, mouseCursorPosition: pt, mouseButton: button) {
            d.setIntegerValueField(.mouseEventClickState, value: Int64(i))
            d.post(tap: .cghidEventTap)
        }
        if let u = CGEvent(mouseEventSource: nil, mouseType: up, mouseCursorPosition: pt, mouseButton: button) {
            u.setIntegerValueField(.mouseEventClickState, value: Int64(i))
            u.post(tap: .cghidEventTap)
        }
    }
}

func synthDrag(_ a: ActionWire, _ session: Session) throws {
    let start = try actionPoint(a, session)
    guard let toX = a.to_x, let toY = a.to_y else {
        throw DaemonError(code: Code.accessibilityError, message: "drag needs to_x and to_y")
    }
    let end = CGPoint(x: toX, y: toY)
    try requirePointInFocusedWindow(end, bundleID: a.bundle_id)
    guard let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                             mouseCursorPosition: start, mouseButton: .left),
          let move = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDragged,
                             mouseCursorPosition: end, mouseButton: .left),
          let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                           mouseCursorPosition: end, mouseButton: .left) else {
        throw DaemonError(code: Code.accessibilityError, message: "cannot create drag events")
    }
    try requireFrontmost(a.bundle_id)
    down.post(tap: .cghidEventTap)
    Thread.sleep(forTimeInterval: 0.08)
    do {
        try requireFrontmost(a.bundle_id)
        move.post(tap: .cghidEventTap)
        Thread.sleep(forTimeInterval: 0.08)
        try requireFrontmost(a.bundle_id)
        up.post(tap: .cghidEventTap)
    } catch {
        // Never leave the global mouse button logically held if takeover is
        // detected mid-drag. A lone mouse-up does not apply the intended drag.
        up.post(tap: .cghidEventTap)
        throw error
    }
}

func synthType(_ text: String, bundleID: String) throws {
    for scalar in text.unicodeScalars {
        try requireFrontmost(bundleID)
        var ch = UniChar(scalar.value & 0xffff)
        if let d = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true) {
            d.keyboardSetUnicodeString(stringLength: 1, unicodeString: &ch)
            d.post(tap: .cghidEventTap)
        }
        if let u = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false) {
            u.keyboardSetUnicodeString(stringLength: 1, unicodeString: &ch)
            u.post(tap: .cghidEventTap)
        }
    }
}

// synthKey handles a chord like "cmd+s". A minimal keymap covers the common
// keys; a full xdotool-style map is phase-2 polish.
func synthKey(_ chord: String, bundleID: String) throws {
    let parts = chord.lowercased().split(separator: "+").map(String.init)
    var flags: CGEventFlags = []
    var keyCode: CGKeyCode?
    for p in parts {
        switch p {
        case "cmd", "command": flags.insert(.maskCommand)
        case "ctrl", "control": flags.insert(.maskControl)
        case "opt", "alt", "option": flags.insert(.maskAlternate)
        case "shift": flags.insert(.maskShift)
        default: keyCode = keyCodeFor(p)
        }
    }
    guard let kc = keyCode else {
        throw DaemonError(code: Code.unknown, message: "unmapped key in chord: \(chord)")
    }
    try requireFrontmost(bundleID)
    if let d = CGEvent(keyboardEventSource: nil, virtualKey: kc, keyDown: true) {
        d.flags = flags
        d.post(tap: .cghidEventTap)
    }
    if let u = CGEvent(keyboardEventSource: nil, virtualKey: kc, keyDown: false) {
        u.flags = flags
        u.post(tap: .cghidEventTap)
    }
}

func synthScroll(_ a: ActionWire, _ session: Session) throws {
    let dir = a.direction ?? "down"
    let amount: Int32 = (dir == "up" || dir == "left") ? 3 : -3
    let vertical = (dir == "up" || dir == "down")
    let point: CGPoint
    if a.ref != nil || (a.x != nil && a.y != nil) {
        point = try actionPoint(a, session)
    } else {
        let frame = try focusedWindowFrame(a.bundle_id)
        point = CGPoint(x: frame.midX, y: frame.midY)
    }
    try requireFrontmost(a.bundle_id)
    if let e = CGEvent(scrollWheelEvent2Source: nil, units: .line,
                       wheelCount: 1, wheel1: vertical ? amount : 0, wheel2: vertical ? 0 : amount, wheel3: 0) {
        e.location = point
        e.post(tap: .cghidEventTap)
    }
}

// keyCodeFor maps a few common keys. Enough for return/tab/escape and letters;
// the full map is phase-2 work.
func keyCodeFor(_ k: String) -> CGKeyCode? {
    let map: [String: CGKeyCode] = [
        "return": 36, "enter": 36, "tab": 48, "space": 49, "escape": 53, "esc": 53,
        "delete": 51, "left": 123, "right": 124, "down": 125, "up": 126,
        "a": 0, "s": 1, "d": 2, "f": 3, "c": 8, "v": 9, "z": 6, "w": 13, "n": 45, "q": 12,
    ]
    return map[k]
}

// MARK: - Capture

func captureHelperURL() -> URL? {
    if let override = ProcessInfo.processInfo.environment["JCODE_COMPUTERD_CAPTURE"],
       FileManager.default.isExecutableFile(atPath: override) {
        return URL(fileURLWithPath: override)
    }
    let daemon = URL(fileURLWithPath: CommandLine.arguments[0]).standardizedFileURL
    let name = daemon.lastPathComponent
    let prefix = "jcode-computerd"
    guard name.hasPrefix(prefix) else { return nil }
    // jcode-computerd-aarch64-apple-darwin ->
    // jcode-computerd-capture-aarch64-apple-darwin.
    let suffix = String(name.dropFirst(prefix.count))
    let sibling = daemon.deletingLastPathComponent().appendingPathComponent(prefix + "-capture" + suffix)
    if FileManager.default.isExecutableFile(atPath: sibling.path) { return sibling }
    let unsuffixed = daemon.deletingLastPathComponent().appendingPathComponent(prefix + "-capture")
    return FileManager.default.isExecutableFile(atPath: unsuffixed.path) ? unsuffixed : nil
}

// MARK: - Disclaimed worker spawn (TCC responsibility)
//
// Screen Recording consent is keyed on the *responsible process*, and a
// spawned child inherits its parent's responsibility by default: launched from
// the desktop app, the capture worker's prompts and grants land on
// jcode-desktop; launched from a terminal, on the terminal app — verified
// against tccd's AttributionChain logging. responsibility_spawnattrs_setdisclaim
// makes the worker responsible for itself, so Screen Recording consent always
// attaches to the worker's own code identity (the jcode-computerd.app bundle),
// no matter which process launched jcode. Chromium ships the same mechanism
// for its own helpers; the symbol is stable libSystem SPI since long before
// our macOS 14 floor.

@_silgen_name("responsibility_spawnattrs_setdisclaim")
func responsibility_spawnattrs_setdisclaim(
    _ attrs: UnsafeMutablePointer<posix_spawnattr_t?>, _ disclaim: Int32) -> Int32

// _NSGetEnviron returns the C global `environ` (char***) through Foundation's
// bridge. Swift does not see the symbol directly, so declare it via the linker
// name — the same mechanism used for the disclaim SPI above.
@_silgen_name("_NSGetEnviron")
func _NSGetEnviron() -> UnsafeMutablePointer<UnsafeMutablePointer<UnsafeMutablePointer<CChar>?>?>?

final class WorkerProcess {
    let pid: pid_t
    private var reaped: Int32?

    init(pid: pid_t) { self.pid = pid }

    /// The raw waitpid status once the child has exited, nil while running.
    /// Unlike kill(pid, 0) this is zombie-correct: reaping counts as exited.
    @discardableResult
    func pollStatus() -> Int32? {
        if let s = reaped { return s }
        var status: Int32 = 0
        if waitpid(pid, &status, WNOHANG) == pid {
            reaped = status
            return status
        }
        return nil
    }

    var isRunning: Bool { pollStatus() == nil }

    func terminate() { kill(pid, SIGTERM) }
    func killNow() { kill(pid, SIGKILL) }

    /// Mirrors Process.terminationStatus (exit code on normal exit, the
    /// terminating signal otherwise). Blocks until the child is reaped.
    var terminationStatus: Int32 {
        while reaped == nil {
            _ = pollStatus()
            if reaped == nil { Thread.sleep(forTimeInterval: 0.01) }
        }
        let status = reaped!
        if (status & 0x7f) == 0 { return (status >> 8) & 0xff } // WIFEXITED
        return status & 0x7f // WTERMSIG
    }

    /// Reap without blocking this thread. Used when a consent prompt keeps the
    /// worker alive past the probe bound — killing it would dismiss the system
    /// dialog the user is about to answer, and not reaping it would zombie.
    func reapInBackground() {
        DispatchQueue.global().async { _ = self.terminationStatus }
    }
}

// spawnDisclaimedWorker starts the capture worker with its own TCC
// responsibility (see above). stdout/stderr are wired exactly like
// Foundation's Process does: the parent's write ends are closed at spawn so
// the readers observe EOF when the child exits.
func spawnDisclaimedWorker(
    executable: URL, arguments: [String], stdout: Pipe, stderr: Pipe
) throws -> WorkerProcess {
    // posix_spawnattr_t is void* on macOS, and the Darwin imports want a
    // pointer to the *optional* form. A stack-local optional gives us that
    // pointer for free, no manual allocation needed.
    var attr: posix_spawnattr_t? = nil
    guard posix_spawnattr_init(&attr) == 0 else {
        throw DaemonError(code: Code.unknown, message: "posix_spawnattr_init failed")
    }
    defer { posix_spawnattr_destroy(&attr) }
    _ = responsibility_spawnattrs_setdisclaim(&attr, 1)

    var actions: posix_spawn_file_actions_t? = nil
    guard posix_spawn_file_actions_init(&actions) == 0 else {
        throw DaemonError(code: Code.unknown, message: "posix_spawn_file_actions_init failed")
    }
    defer { posix_spawn_file_actions_destroy(&actions) }
    posix_spawn_file_actions_adddup2(&actions, stdout.fileHandleForWriting.fileDescriptor, STDOUT_FILENO)
    posix_spawn_file_actions_adddup2(&actions, stderr.fileHandleForWriting.fileDescriptor, STDERR_FILENO)
    posix_spawn_file_actions_addclose(&actions, stdout.fileHandleForReading.fileDescriptor)
    posix_spawn_file_actions_addclose(&actions, stderr.fileHandleForReading.fileDescriptor)

    var argv: [UnsafeMutablePointer<CChar>?] =
        ([executable.path] + arguments).map { strdup($0) } + [nil]
    defer { for a in argv { free(a) } }
    var pid = pid_t()
    let envp = _NSGetEnviron()!.pointee
    let rc = posix_spawn(&pid, executable.path, &actions, &attr, &argv, envp)
    try? stdout.fileHandleForWriting.close()
    try? stderr.fileHandleForWriting.close()
    guard rc == 0 else {
        throw DaemonError(code: Code.unknown,
                          message: "spawn capture worker: \(String(cString: strerror(rc)))")
    }
    return WorkerProcess(pid: pid)
}

func captureWorkerPermissionState() -> String {
    guard let helper = captureHelperURL() else { return "unknown" }
    let stdout = Pipe()
    let process: WorkerProcess
    do {
        process = try spawnDisclaimedWorker(
            executable: helper, arguments: ["--check-permission"], stdout: stdout, stderr: Pipe())
    } catch {
        return "unknown"
    }

    let deadline = Date().addingTimeInterval(2)
    while process.isRunning && Date() < deadline { Thread.sleep(forTimeInterval: 0.01) }
    if process.isRunning {
        process.terminate()
        Thread.sleep(forTimeInterval: 0.05)
        if process.isRunning { process.killNow() }
        _ = process.terminationStatus
        return "unknown"
    }
    let raw = stdout.fileHandleForReading.readDataToEndOfFile()
    guard process.terminationStatus == 0 else { return "unknown" }
    let state = String(data: raw, encoding: .utf8)?
        .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    switch state {
    case "granted", "denied": return state
    default: return "unknown"
    }
}

// requestCaptureWorkerPermission asks the capture worker to surface the system
// Screen Recording consent prompt for its own executable identity (the grant
// belongs to the worker, not this daemon — see WindowCaptureHelper.swift). The
// prompt is asynchronous. If the worker is still waiting on the dialog past
// the probe bound it is left running — killing it would dismiss the alert the
// user is about to answer — and reaped in the background so it cannot zombie.
// In that case the state is necessarily "denied": an already-granted worker
// prints "granted" and exits immediately without ever prompting.
@discardableResult
func requestCaptureWorkerPermission() -> String {
    guard let helper = captureHelperURL() else { return "unknown" }
    let stdout = Pipe()
    let process: WorkerProcess
    do {
        process = try spawnDisclaimedWorker(
            executable: helper, arguments: ["--request-permission"], stdout: stdout, stderr: Pipe())
    } catch {
        return "unknown"
    }

    let deadline = Date().addingTimeInterval(3)
    while process.isRunning && Date() < deadline { Thread.sleep(forTimeInterval: 0.01) }
    if process.isRunning {
        process.reapInBackground()
        return "denied"
    }
    let raw = stdout.fileHandleForReading.readDataToEndOfFile()
    guard process.terminationStatus == 0 else { return "unknown" }
    let state = String(data: raw, encoding: .utf8)?
        .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    switch state {
    case "granted", "denied": return state
    default: return "unknown"
    }
}

func handleCapture(_ req: AppRequest, _ session: Session) throws -> CaptureResult {
    try checkScreenUnlocked()
    let app = try requireFrontmost(req.app)

    guard #available(macOS 14.0, *) else {
        throw DaemonError(code: Code.unknown, message: "screenshot requires macOS 14+")
    }
    guard let helper = captureHelperURL() else {
        throw DaemonError(code: Code.unknown,
                          message: "jcode-computerd-capture not found next to the daemon")
    }

    try? FileManager.default.createDirectory(atPath: session.shotsDir, withIntermediateDirectories: true)
    let id = UUID().uuidString
    let path = (session.shotsDir as NSString).appendingPathComponent("\(id).png")
    do {
        var arguments = ["--pid", String(app.processIdentifier), "--output", path]
        // The AX tools and screenshot must describe the same target. Pass the
        // focused/main AX window as a title+bounds hint; the capture worker uses
        // it to disambiguate multi-window apps instead of blindly taking the
        // largest background window. If AX is unavailable, it safely falls
        // back to the largest app window so screenshot-only diagnosis remains
        // possible with Screen Recording permission alone.
        let targetWindow = accessibilityRoot(app)
        let title = axString(targetWindow, kAXTitleAttribute)
        if !title.isEmpty { arguments += ["--window-title", title] }
        if let frame = elementFrame(targetWindow), frame.width > 1, frame.height > 1 {
            arguments += [
                "--window-x", String(Double(frame.origin.x)),
                "--window-y", String(Double(frame.origin.y)),
                "--window-width", String(Double(frame.width)),
                "--window-height", String(Double(frame.height)),
            ]
        }
        let stdout = Pipe()
        let stderr = Pipe()
        let process = try spawnDisclaimedWorker(
            executable: helper, arguments: arguments, stdout: stdout, stderr: stderr)

        let deadline = Date().addingTimeInterval(10)
        while process.isRunning && Date() < deadline { Thread.sleep(forTimeInterval: 0.02) }
        if process.isRunning {
            process.terminate()
            Thread.sleep(forTimeInterval: 0.1)
            if process.isRunning { process.killNow() }
            _ = process.terminationStatus
            try? FileManager.default.removeItem(atPath: path)
            throw DaemonError(code: Code.unknown, message: "window capture helper timed out")
        }

        let detail = String(data: stderr.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let workerData = stdout.fileHandleForReading.readDataToEndOfFile()
        guard process.terminationStatus == 0 else {
            try? FileManager.default.removeItem(atPath: path)
            // A failed capture is overwhelmingly a missing Screen Recording
            // grant. Confirm with a probe (failures like "no capturable window"
            // must not fire a spurious prompt), then surface the consent dialog
            // once so the user can fix it in context rather than finding the
            // right pane by themselves.
            if !didAutoPromptScreenRecording, captureWorkerPermissionState() == "denied" {
                didAutoPromptScreenRecording = true
                if !surfaceOnboardingUI() { _ = requestCaptureWorkerPermission() }
            }
            let suffix = detail.isEmpty ? "status \(process.terminationStatus)" : detail
            // Name the identity the user will actually find in System
            // Settings: the branded bundle row for bundle installs, the bare
            // binary for dev runs.
            let identity = isRunningFromHelperBundle() ? "jcode Computer Use" : "jcode-computerd-capture"
            throw DaemonError(code: Code.permissionsNotGranted,
                              message: "window capture failed — Screen Recording permission may not be granted for \(identity). A permission window or macOS consent prompt was shown if the grant is missing; approve it or enable \(identity) under System Settings › Privacy & Security › Screen Recording (\(suffix))")
        }
        do {
            try checkScreenUnlocked()
        } catch {
            try? FileManager.default.removeItem(atPath: path)
            throw error
        }
        let attrs = try FileManager.default.attributesOfItem(atPath: path)
        let byteCount = (attrs[.size] as? NSNumber)?.int64Value ?? 0
        guard byteCount > 0, byteCount <= maxCaptureBytes else {
            try? FileManager.default.removeItem(atPath: path)
            throw DaemonError(code: Code.unknown,
                              message: "capture helper produced \(byteCount) bytes; maximum is \(maxCaptureBytes)")
        }
        let file = try FileHandle(forReadingFrom: URL(fileURLWithPath: path))
        let header = try file.read(upToCount: 8) ?? Data()
        try? file.close()
        guard header.elementsEqual([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]) else {
            try? FileManager.default.removeItem(atPath: path)
            throw DaemonError(code: Code.unknown, message: "capture helper produced an invalid PNG")
        }
        guard let metadata = try? JSONDecoder().decode(CaptureWorkerResult.self, from: workerData) else {
            try? FileManager.default.removeItem(atPath: path)
            throw DaemonError(code: Code.unknown, message: "capture helper returned invalid window metadata")
        }
        let current = try requireFrontmost(req.app)
        let currentWindow = accessibilityRoot(current)
        guard current.processIdentifier == app.processIdentifier,
              CFEqual(currentWindow, targetWindow) else {
            try? FileManager.default.removeItem(atPath: path)
            throw DaemonError(code: Code.userIntervened,
                              message: "process or focused window changed during screenshot capture")
        }
        session.bindWindow(
            req.app, processIdentifier: app.processIdentifier, rootWindow: targetWindow)
        return CaptureResult(
            ref: path, png: nil,
            x: metadata.x, y: metadata.y, width: metadata.width, height: metadata.height,
            pixel_width: metadata.pixel_width, pixel_height: metadata.pixel_height)
    } catch let error as DaemonError {
        throw error
    } catch {
        try? FileManager.default.removeItem(atPath: path)
        throw DaemonError(code: Code.unknown, message: "start window capture helper: \(error.localizedDescription)")
    }
}

let maxCaptureBytes: Int64 = 20 * 1024 * 1024

// MARK: - Session + dispatch

final class Session {
    private var registries: [String: ElementRegistry] = [:]
    private var windowBindings: [String: WindowBinding] = [:]
    var gen = 0
    let shotsDir: String
    init(shotsDir: String) { self.shotsDir = shotsDir }

    func registry(
        for app: String, processIdentifier: pid_t, rootWindow: AXUIElement
    ) -> ElementRegistry {
        if let existing = registries[app],
           existing.matches(processIdentifier: processIdentifier, rootWindow: rootWindow) {
            return existing
        }
        let r = ElementRegistry(processIdentifier: processIdentifier, rootWindow: rootWindow)
        registries[app] = r
        return r
    }

    func boundRegistry(for app: String) -> ElementRegistry? { registries[app] }

    func bindWindow(_ app: String, processIdentifier: pid_t, rootWindow: AXUIElement) {
        if let registry = registries[app],
           !registry.matches(processIdentifier: processIdentifier, rootWindow: rootWindow) {
            // A screenshot may observe a new window without rebuilding its AX
            // tree. Drop refs from the old window so none can target it later.
            registries.removeValue(forKey: app)
        }
        windowBindings[app] = WindowBinding(
            processIdentifier: processIdentifier, rootWindow: rootWindow)
    }

    func matchesBoundWindow(
        _ app: String, processIdentifier: pid_t, rootWindow: AXUIElement
    ) -> Bool {
        guard let binding = windowBindings[app] else { return false }
        return binding.processIdentifier == processIdentifier && CFEqual(binding.rootWindow, rootWindow)
    }
}

struct WindowBinding {
    let processIdentifier: pid_t
    let rootWindow: AXUIElement
}

func dispatch(_ req: Envelope, _ session: Session) -> Envelope {
    do {
        currentAXFatalError = nil
        switch req.type {
        case "list_apps":
            return Envelope(type: "result", id: req.id, payload: encodePayload(handleListApps()))
        case "frontmost":
            return Envelope(type: "result", id: req.id, payload: encodePayload(try handleFrontmost()))
        case "tree":
            let r = try decodePayload(TreeRequest.self, req.payload)
            return Envelope(type: "result", id: req.id, payload: encodePayload(try handleTree(r, session)))
        case "capture":
            let r = try decodePayload(AppRequest.self, req.payload)
            return Envelope(type: "result", id: req.id, payload: encodePayload(try handleCapture(r, session)))
        case "launch":
            try checkScreenUnlocked()
            let r = try decodePayload(AppRequest.self, req.payload)
            try handleLaunch(r)
            settleUI()
            return Envelope(type: "result", id: req.id, payload: encodePayload([String: String]()))
        case "read_clipboard":
            try checkScreenUnlocked()
            return Envelope(type: "result", id: req.id, payload: encodePayload(handleReadClipboard()))
        case "request_permissions":
            let r = try decodePayload(RequestPermissionsPayload.self, req.payload)
            return Envelope(type: "result", id: req.id, payload: encodePayload(handleRequestPermissions(r)))
        case "perform":
            let r = try decodePayload(PerformRequest.self, req.payload)
            try handlePerform(r, session)
            return Envelope(type: "result", id: req.id, payload: encodePayload([String: String]()))
        default:
            return errorEnvelope(req.id, Code.unknown, "unknown request type: \(req.type)")
        }
    } catch let e as DaemonError {
        return errorEnvelope(req.id, e.code, e.message)
    } catch {
        return errorEnvelope(req.id, Code.unknown, error.localizedDescription)
    }
}

func errorEnvelope(_ id: UInt64, _ code: Int, _ msg: String) -> Envelope {
    Envelope(type: "error", id: id, payload: encodePayload(ErrorPayload(code: code, message: msg)))
}

// MARK: - Server (unix socket, token auth)

func currentPong() -> PongPayload {
    // These calls only inspect TCC state; neither asks the user or opens System
    // Settings. Accessibility belongs to this long-lived AX daemon. Screen
    // Recording belongs to the separate executable that actually calls
    // ScreenCaptureKit, so query that worker instead of sampling this process
    // and risking a false-green result under identity-scoped TCC.
    PongPayload(
        server_api_version: apiVersion,
        platform: "darwin",
        helper_version: helperVersion,
        accessibility_permission: AXIsProcessTrusted() ? "granted" : "denied",
        screen_recording_permission: captureWorkerPermissionState())
}

func handlePing(_ req: Envelope, token: String) -> Envelope {
    guard let ping = try? decodePayload(PingPayload.self, req.payload) else {
        return errorEnvelope(req.id, Code.unknown, "invalid ping payload")
    }
    if ping.token != token {
        return errorEnvelope(req.id, Code.senderNotAuthenticated, "bad token")
    }
    if ping.client_api_version != apiVersion {
        return errorEnvelope(req.id, Code.incompatibleVersion, "version mismatch")
    }
    return Envelope(type: "pong", id: req.id, payload: encodePayload(currentPong()))
}

func serveConnection(_ fd: Int32, token: String, shotsDir: String) {
    defer { close(fd) }
    let session = Session(shotsDir: shotsDir)

    // runServer has already checked that the kernel-reported peer PID is the
    // jcode process that spawned this daemon. The token is a second factor for
    // protocol authentication; neither a readable same-uid socket nor the
    // long-lived token file alone is accepted as authority to drive TCC-granted
    // UI automation.
    guard let first = try? readFrame(fd), first.type == "ping" else {
        return
    }
    let firstResponse = handlePing(first, token: token)
    guard (try? writeFrame(fd, firstResponse)) != nil, firstResponse.type == "pong" else { return }

    // Serve requests until the client disconnects.
    while let req = try? readFrame(fd) {
        if req.type == "ping" {
            // Re-sample both grants so a settings poll can observe a permission
            // change without restarting either process. A bad re-authentication
            // attempt terminates this connection after its error response.
            let resp = handlePing(req, token: token)
            if (try? writeFrame(fd, resp)) == nil || resp.type != "pong" { return }
        } else {
            let resp = dispatch(req, session)
            if (try? writeFrame(fd, resp)) == nil { return }
        }
    }
}

let helperVersion = "0.1.0"

func peerPID(_ fd: Int32) -> pid_t? {
    var value: pid_t = 0
    var length = socklen_t(MemoryLayout<pid_t>.size)
    let result = withUnsafeMutablePointer(to: &value) { pointer in
        getsockopt(fd, SOL_LOCAL, LOCAL_PEERPID, pointer, &length)
    }
    return result == 0 ? value : nil
}

func processIsAlive(_ pid: pid_t) -> Bool {
    errno = 0
    if kill(pid, 0) == 0 { return true }
    // EPERM still proves that a process owns the PID; only ESRCH is dead.
    return errno != ESRCH
}

let handoffInstanceHexLength = 32
let legacyHandoffCleanupGrace: TimeInterval = 10 * 60

struct HandoffDirectoryOwner {
    let pid: pid_t
    let legacy: Bool
}

// Accept the migration format handoff-PID and the process-instance format
// handoff-PID-<128-bit-lowercase-hex>. A strict parser keeps similarly named
// user files outside this daemon's ownership boundary.
func parseHandoffDirectoryOwner(_ name: String) -> HandoffDirectoryOwner? {
    let prefix = "handoff-"
    guard name.hasPrefix(prefix) else { return nil }
    let suffix = name.dropFirst(prefix.count)
    let parts = suffix.split(separator: "-", omittingEmptySubsequences: false)
    guard parts.count == 1 || parts.count == 2,
          let owner = Int32(String(parts[0])),
          owner > 1 else { return nil }
    if parts.count == 1 {
        return HandoffDirectoryOwner(pid: owner, legacy: true)
    }
    let instance = parts[1]
    guard instance.utf8.count == handoffInstanceHexLength,
          instance.utf8.allSatisfy({ byte in
              (byte >= 48 && byte <= 57) || (byte >= 97 && byte <= 102)
          }) else { return nil }
    return HandoffDirectoryOwner(pid: owner, legacy: false)
}

func legacyHandoffIsOldEnough(_ entry: URL, now: Date = Date()) -> Bool {
    guard let values = try? entry.resourceValues(forKeys: [.contentModificationDateKey]),
          let modified = values.contentModificationDate else { return false }
    return now.timeIntervalSince(modified) >= legacyHandoffCleanupGrace
}

// Recover process-instance handoff directories left by clients that crashed
// before their daemon could exit. The exact current path is never swept. New
// nonce names remain distinct across PID reuse; legacy PID-only names get an
// age grace and a second liveness check during the migration window.
func cleanupStaleHandoffDirectories(shotsDir: String, currentClientPID: pid_t) {
    let manager = FileManager.default
    let current = URL(fileURLWithPath: shotsDir).standardizedFileURL
    let parent = current.deletingLastPathComponent()
    guard let entries = try? manager.contentsOfDirectory(
        at: parent,
        includingPropertiesForKeys: nil,
        options: [.skipsHiddenFiles]
    ) else { return }
    for entry in entries {
        let name = entry.lastPathComponent
        guard entry.standardizedFileURL.path != current.path,
              let parsed = parseHandoffDirectoryOwner(name) else { continue }
        if parsed.pid == currentClientPID {
            // Same PID but a different nonce/legacy name can only belong to a
            // previous incarnation; the exact current path was skipped above.
            try? manager.removeItem(at: entry)
            continue
        }
        guard !processIsAlive(parsed.pid) else { continue }
        if parsed.legacy && !legacyHandoffIsOldEnough(entry) { continue }
        // Narrow the legacy dead-check/remove race. Nonce paths do not collide
        // with a new incarnation even if the numeric PID is reused here.
        guard !processIsAlive(parsed.pid) else { continue }
        try? manager.removeItem(at: entry)
    }
}

func runServer(socketPath: String, tokenFile: String, shotsDir: String, clientPID: pid_t) {
    // A canceled Go RPC may close its socket before this process writes the
    // response. Treat EPIPE as a normal connection failure; the default SIGPIPE
    // action would otherwise terminate the whole long-lived AX daemon.
    signal(SIGPIPE, SIG_IGN)
    // Set the process-wide default used by AX calls. A hung target app must not
    // pin the serial daemon forever after the Go socket deadline has elapsed.
    _ = AXUIElementSetMessagingTimeout(AXUIElementCreateSystemWide(), 3.0)

    let fileManager = FileManager.default
    cleanupStaleHandoffDirectories(shotsDir: shotsDir, currentClientPID: clientPID)
    // This exact path belongs only to one client process instance. Remove a
    // reconnect orphan before serving, and clean all normal-return paths
    // (including idle exit).
    try? fileManager.removeItem(atPath: shotsDir)
    defer { try? fileManager.removeItem(atPath: shotsDir) }

    guard let tokenData = try? String(contentsOfFile: tokenFile, encoding: .utf8) else {
        FileHandle.standardError.write("cannot read token file: \(tokenFile)\n".data(using: .utf8)!)
        exit(1)
    }
    let token = tokenData.trimmingCharacters(in: .whitespacesAndNewlines)

    // sun_path is 104 bytes on macOS; a longer path would be silently truncated
    // and bind to the wrong place. Fail loudly instead — the production path
    // (~/.jcode/computer/computerd.sock) is well under this, so hitting it means
    // something is wrong.
    if socketPath.utf8.count > 103 {
        FileHandle.standardError.write("socket path too long (\(socketPath.utf8.count) > 103): \(socketPath)\n".data(using: .utf8)!)
        exit(1)
    }

    unlink(socketPath)
    let fd = socket(AF_UNIX, SOCK_STREAM, 0)
    if fd < 0 { perror("socket"); exit(1) }
    defer {
        close(fd)
        unlink(socketPath)
    }

    var addr = sockaddr_un()
    addr.sun_family = sa_family_t(AF_UNIX)
    _ = socketPath.withCString { cstr -> Int in
        _ = withUnsafeMutablePointer(to: &addr.sun_path) {
            $0.withMemoryRebound(to: CChar.self, capacity: 104) { dst in
                strncpy(dst, cstr, 103)
            }
        }
        return 0
    }
    let len = socklen_t(MemoryLayout<sockaddr_un>.size)
    let bindResult = withUnsafePointer(to: &addr) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, len) }
    }
    if bindResult < 0 { perror("bind"); exit(1) }
    // Only the owner may connect; belt to the token's suspenders.
    chmod(socketPath, 0o600)
    if listen(fd, 4) < 0 { perror("listen"); exit(1) }

    // Idle self-exit: a crashed jcode must not leave an automation daemon running
    // (design §5, §8 — it bounds the window in which the daemon exists). accept()
    // is given a receive timeout; if no client connects within the idle window,
    // the daemon exits. The timeout resets every time a client connects and
    // disconnects, so an active session keeps it alive.
    // The idle window is overridable via env (milliseconds) so the timeout is
    // testable without waiting the full production interval. poll() is used
    // rather than SO_RCVTIMEO because the latter's effect on accept() is not
    // reliable across platforms; poll on the listening fd is.
    let idleMS = Int32(Int(ProcessInfo.processInfo.environment["JCODE_COMPUTERD_IDLE_MS"] ?? "")
        ?? (idleTimeoutSeconds * 1000))

    while true {
        var pfd = pollfd(fd: fd, events: Int16(POLLIN), revents: 0)
        let pr = poll(&pfd, 1, idleMS)
        if pr == 0 {
            // Idle window elapsed with no connection. Exit cleanly.
            return
        }
        if pr < 0 {
            if errno == EINTR { continue }
            continue
        }
        let client = accept(fd, nil, nil)
        if client < 0 { continue }
        guard peerPID(client) == clientPID else {
            close(client)
            continue
        }
        // One connection at a time — UI automation is a serial resource, and the
        // Go client already serializes; a second connection would race the AX
        // state. Handle inline rather than spawning a thread.
        serveConnection(client, token: token, shotsDir: shotsDir)
    }
}

// idleTimeoutSeconds bounds how long the daemon waits for a connection before
// exiting. Long enough that a user pausing between tasks doesn't pay a respawn;
// short enough that a crashed jcode's daemon doesn't linger.
let idleTimeoutSeconds = 300

// MARK: - main

func parseFlag(_ name: String) -> String? {
    let args = CommandLine.arguments
    for i in 0..<args.count where args[i] == name && i + 1 < args.count {
        return args[i + 1]
    }
    return nil
}

guard let socketPath = parseFlag("--socket"),
      let tokenFile = parseFlag("--token-file"),
      let shotsDir = parseFlag("--shots-dir"),
      let clientPIDText = parseFlag("--client-pid"),
      let clientPID = Int32(clientPIDText), clientPID > 1 else {
    FileHandle.standardError.write("usage: jcode-computerd --socket <path> --token-file <path> --shots-dir <path> --client-pid <pid>\n".data(using: .utf8)!)
    exit(2)
}
// After flag validation (usage errors print once), before the socket exists.
maybeReexecSelfResponsible()
runServer(socketPath: socketPath, tokenFile: tokenFile, shotsDir: shotsDir, clientPID: clientPID)
