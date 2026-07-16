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
import ScreenCaptureKit

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
    case null, bool(Bool), number(Double), string(String)
    case array([JSONValue]), object([String: JSONValue])

    init(from d: Decoder) throws {
        let c = try d.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
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
struct CaptureResult: Codable { var ref: String?; var png: Data? }
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

func handleListApps() -> ListAppsResult {
    let apps = NSWorkspace.shared.runningApplications.compactMap { app -> AppWire? in
        guard let bundle = app.bundleIdentifier else { return nil }
        // Only regular apps have a UI worth automating; skip agents/daemons.
        guard app.activationPolicy == .regular else { return nil }
        return AppWire(bundle_id: bundle, name: app.localizedName ?? bundle, running: true)
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
    let sem = DispatchSemaphore(value: 0)
    var launchErr: Error?
    NSWorkspace.shared.openApplication(at: url, configuration: cfg) { _, err in
        launchErr = err
        sem.signal()
    }
    _ = sem.wait(timeout: .now() + 10)
    if let e = launchErr { throw DaemonError(code: Code.unknown, message: e.localizedDescription) }
}

func handleReadClipboard() -> ReadClipboardResult {
    ReadClipboardResult(text: NSPasteboard.general.string(forType: .string) ?? "")
}

// MARK: - Accessibility (needs the Accessibility TCC grant)

func runningApp(_ bundleID: String) throws -> NSRunningApplication {
    let matches = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID)
    if matches.isEmpty { throw DaemonError(code: Code.appNotAllowed, message: bundleID) }
    if matches.count > 1 { /* pick the frontmost-ish; ambiguity is rare for regular apps */ }
    return matches[0]
}

// axString reads a string attribute, "" when absent.
func axString(_ el: AXUIElement, _ attr: String) -> String {
    var v: CFTypeRef?
    if AXUIElementCopyAttributeValue(el, attr as CFString, &v) == .success, let s = v as? String {
        return s
    }
    return ""
}

// axBatch reads several attributes of one element in a single cross-process call
// via AXUIElementCopyMultipleAttributeValues — the difference between a snapshot
// taking 200ms and 2s on a large tree, because each individual attribute read is
// its own cross-process round-trip (design §3.3). Missing attributes come back as
// an AXError placeholder, which simply fails the `as?` cast below → default.
func axBatch(_ el: AXUIElement, _ attrs: [String]) -> [CFTypeRef] {
    var values: CFArray?
    let r = AXUIElementCopyMultipleAttributeValues(
        el, attrs as CFArray, AXCopyMultipleAttributeOptions(rawValue: 0), &values)
    guard r == .success, let arr = values as? [CFTypeRef], arr.count == attrs.count else {
        return []
    }
    return arr
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
    private var byElement: [ElementKey: Int64] = [:]
    private var byRef: [Int64: AXUIElement] = [:]
    private var nextRef: Int64 = 100

    func refFor(_ el: AXUIElement) -> Int64 {
        let key = ElementKey(element: el)
        if let r = byElement[key] { return r }
        nextRef += 1
        byElement[key] = nextRef
        byRef[nextRef] = el
        return nextRef
    }

    func element(_ ref: Int64) -> AXUIElement? { byRef[ref] }
}

// TreeBuilder walks an app's AX tree into flat Nodes, one batched read per node,
// assigning each actionable element a session-stable Ref from the registry.
final class TreeBuilder {
    private(set) var nodes: [Node] = []
    private let registry: ElementRegistry
    private var nextID = 0

    // The attributes read per node, in a fixed order axBatch returns them in.
    private let attrs: [String] = [
        kAXRoleAttribute, kAXTitleAttribute, kAXValueAttribute,
        kAXFocusedAttribute, kAXEnabledAttribute, kAXChildrenAttribute,
    ]

    init(registry: ElementRegistry) { self.registry = registry }

    func build(_ root: AXUIElement) { _ = walk(root) }

    private func walk(_ el: AXUIElement) -> String {
        nextID += 1
        let id = String(nextID)

        let v = axBatch(el, attrs) // one cross-process round-trip for this node
        let role = v.count > 0 ? (v[0] as? String ?? "") : axString(el, kAXRoleAttribute)
        let title = v.count > 1 ? (v[1] as? String ?? "") : ""
        let value = v.count > 2 ? (v[2] as? String ?? "") : ""
        let focused = v.count > 3 ? (v[3] as? Bool ?? false) : false
        let enabled = v.count > 4 ? (v[4] as? Bool ?? true) : true
        let children = v.count > 5 ? (v[5] as? [AXUIElement] ?? []) : []

        var ref: Int64 = 0
        // Only actionable elements get a ref (mirrors uitree: a node the backend
        // can't resolve should not get a uid).
        if isActionable(role) { ref = registry.refFor(el) }

        var childIDs: [String] = []
        for k in children { childIDs.append(walk(k)) }

        var states: [NodeState] = []
        if focused { states.append(NodeState(Name: "focused", Value: "true")) }
        if !enabled { states.append(NodeState(Name: "disabled", Value: "true")) }

        nodes.append(Node(
            ID: id, Role: role, Name: title, Value: value, States: states,
            ChildIDs: childIDs, Ref: ref, Ignored: false))
        return id
    }

    private func isActionable(_ role: String) -> Bool {
        switch role {
        case kAXButtonRole, kAXTextFieldRole, kAXTextAreaRole, kAXCheckBoxRole,
             kAXRadioButtonRole, kAXPopUpButtonRole, kAXMenuItemRole,
             kAXComboBoxRole, kAXSliderRole, "AXLink":
            return true
        default:
            return false
        }
    }
}

func handleTree(_ req: TreeRequest, _ session: Session) throws -> TreeResult {
    guard AXIsProcessTrusted() else {
        throw DaemonError(code: Code.permissionsNotGranted,
                          message: "Accessibility permission not granted. Grant it in System Settings › Privacy & Security › Accessibility.")
    }
    try checkScreenUnlocked()
    let app = try runningApp(req.app)
    let axApp = AXUIElementCreateApplication(app.processIdentifier)
    let builder = TreeBuilder(registry: session.registry(for: req.app))
    builder.build(axApp)
    session.gen += 1
    return TreeResult(nodes: builder.nodes, gen: session.gen)
}

// MARK: - Perform (input synthesis + AX actions)

func handlePerform(_ req: PerformRequest, _ session: Session) throws {
    try checkScreenUnlocked()
    let a = req.action
    switch a.kind {
    case "set_value":
        guard let ref = a.ref, let el = session.registry(for: a.bundle_id).element(ref) else {
            throw DaemonError(code: Code.accessibilityError, message: "no live element for ref")
        }
        let r = AXUIElementSetAttributeValue(el, kAXValueAttribute as CFString, (a.value ?? "") as CFString)
        if r != .success { throw DaemonError(code: Code.accessibilityError, message: "set_value failed: \(r.rawValue)") }
    case "menu":
        guard let ref = a.ref, let el = session.registry(for: a.bundle_id).element(ref), let name = a.name else {
            throw DaemonError(code: Code.accessibilityError, message: "menu needs a live element and an action name")
        }
        let r = AXUIElementPerformAction(el, name as CFString)
        if r != .success { throw DaemonError(code: Code.accessibilityError, message: "action \(name) failed") }
    case "click", "dblclick", "rclick":
        try synthClick(a)
    case "type":
        try synthType(a.text ?? "")
    case "press":
        try synthKey(a.key ?? "")
    case "scroll":
        try synthScroll(a)
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
    if let dict = CGSessionCopyCurrentDictionary() as? [String: Any],
       let locked = dict["CGSSessionScreenIsLocked"] as? Int, locked == 1 {
        throw DaemonError(code: Code.screenLocked, message: "the screen is locked")
    }
}

// synthClick posts a mouse click at the action's coordinates. Input is delivered
// to whatever holds focus — the coordinate carries no target identity — which is
// exactly why the Go side re-checks the frontmost app before every action.
func synthClick(_ a: ActionWire) throws {
    let pt = CGPoint(x: a.x ?? 0, y: a.y ?? 0)
    let (down, up, button): (CGEventType, CGEventType, CGMouseButton)
    if a.kind == "rclick" {
        (down, up, button) = (.rightMouseDown, .rightMouseUp, .right)
    } else {
        (down, up, button) = (.leftMouseDown, .leftMouseUp, .left)
    }
    let clicks = a.kind == "dblclick" ? 2 : 1
    for i in 1...clicks {
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

func synthType(_ text: String) throws {
    for scalar in text.unicodeScalars {
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
func synthKey(_ chord: String) throws {
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
    if let d = CGEvent(keyboardEventSource: nil, virtualKey: kc, keyDown: true) {
        d.flags = flags
        d.post(tap: .cghidEventTap)
    }
    if let u = CGEvent(keyboardEventSource: nil, virtualKey: kc, keyDown: false) {
        u.flags = flags
        u.post(tap: .cghidEventTap)
    }
}

func synthScroll(_ a: ActionWire) throws {
    let dir = a.direction ?? "down"
    let amount: Int32 = (dir == "up" || dir == "left") ? 3 : -3
    let vertical = (dir == "up" || dir == "down")
    if let e = CGEvent(scrollWheelEvent2Source: nil, units: .line,
                       wheelCount: 1, wheel1: vertical ? amount : 0, wheel2: vertical ? 0 : amount, wheel3: 0) {
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

// captureWindow grabs one app window via ScreenCaptureKit — the modern, and now
// only, path (CGWindowListCreateImage was obsoleted in macOS 15). It converts
// points → pixels via SCWindow.frame × the display scale, honoring the design's
// coordinate contract (the helper owns the transform; the Go side sees points).
@available(macOS 14.0, *)
func captureWindow(bundleID: String) async throws -> CGImage {
    let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
    guard let window = content.windows.first(where: {
        $0.owningApplication?.bundleIdentifier == bundleID && $0.isOnScreen
    }) else {
        throw DaemonError(code: Code.unknown, message: "no on-screen window for \(bundleID)")
    }
    let filter = SCContentFilter(desktopIndependentWindow: window)
    let cfg = SCStreamConfiguration()
    // pointPixelScale maps points to pixels; contentRect is in points.
    let scale = filter.pointPixelScale
    cfg.width = Int(filter.contentRect.width * CGFloat(scale))
    cfg.height = Int(filter.contentRect.height * CGFloat(scale))
    return try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: cfg)
}

func handleCapture(_ req: AppRequest, _ session: Session) throws -> CaptureResult {
    _ = try runningApp(req.app) // fail fast if the app isn't running

    guard #available(macOS 14.0, *) else {
        throw DaemonError(code: Code.unknown, message: "screenshot requires macOS 14+")
    }

    // The socket loop is synchronous; bridge to the async SCK API with a
    // semaphore. One capture in flight (the whole protocol is serial), so no
    // contention.
    let sem = DispatchSemaphore(value: 0)
    var image: CGImage?
    var captureErr: Error?
    Task {
        do { image = try await captureWindow(bundleID: req.app) } catch { captureErr = error }
        sem.signal()
    }
    _ = sem.wait(timeout: .now() + 10)
    if let e = captureErr {
        if e is DaemonError { throw e }
        throw DaemonError(code: Code.permissionsNotGranted,
                          message: "window capture failed — Screen Recording permission may not be granted: \(e.localizedDescription)")
    }
    guard let img = image else {
        throw DaemonError(code: Code.unknown, message: "capture produced no image")
    }

    let rep = NSBitmapImageRep(cgImage: img)
    guard let png = rep.representation(using: .png, properties: [:]) else {
        throw DaemonError(code: Code.unknown, message: "PNG encode failed")
    }
    // Write to the shared shots dir and return a reference, keeping the image off
    // the socket (design §3.4).
    let id = UUID().uuidString
    let path = (session.shotsDir as NSString).appendingPathComponent("\(id).png")
    try? FileManager.default.createDirectory(atPath: session.shotsDir, withIntermediateDirectories: true)
    do {
        try png.write(to: URL(fileURLWithPath: path))
        return CaptureResult(ref: path, png: nil)
    } catch {
        return CaptureResult(ref: nil, png: png)
    }
}

// MARK: - Session + dispatch

final class Session {
    private var registries: [String: ElementRegistry] = [:]
    var gen = 0
    let shotsDir: String
    init(shotsDir: String) { self.shotsDir = shotsDir }

    // registry(for:) returns the per-app element registry, creating it on first
    // use. It persists across snapshots so an element keeps its Ref (see
    // ElementRegistry).
    func registry(for app: String) -> ElementRegistry {
        if let r = registries[app] { return r }
        let r = ElementRegistry()
        registries[app] = r
        return r
    }
}

func dispatch(_ req: Envelope, _ session: Session) -> Envelope {
    do {
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
            let r = try decodePayload(AppRequest.self, req.payload)
            try handleLaunch(r)
            return Envelope(type: "result", id: req.id, payload: encodePayload([String: String]()))
        case "read_clipboard":
            return Envelope(type: "result", id: req.id, payload: encodePayload(handleReadClipboard()))
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

func serveConnection(_ fd: Int32, token: String, shotsDir: String) {
    defer { close(fd) }
    let session = Session(shotsDir: shotsDir)

    // First frame must be a ping carrying the token. A unix socket is reachable
    // by any same-uid process, so nothing is served until the token — which
    // lived only in a 0600 file — is presented (design §4).
    guard let first = try? readFrame(fd), first.type == "ping" else {
        return
    }
    guard let ping = try? decodePayload(PingPayload.self, first.payload) else { return }
    if ping.token != token {
        _ = try? writeFrame(fd, errorEnvelope(first.id, Code.senderNotAuthenticated, "bad token"))
        return
    }
    if ping.client_api_version != apiVersion {
        _ = try? writeFrame(fd, errorEnvelope(first.id, Code.incompatibleVersion, "version mismatch"))
        return
    }
    let pong = PongPayload(server_api_version: apiVersion, platform: "darwin", helper_version: helperVersion)
    guard (try? writeFrame(fd, Envelope(type: "pong", id: first.id, payload: encodePayload(pong)))) != nil else { return }

    // Serve requests until the client disconnects.
    while let req = try? readFrame(fd) {
        let resp = dispatch(req, session)
        if (try? writeFrame(fd, resp)) == nil { return }
    }
}

let helperVersion = "0.1.0"

func runServer(socketPath: String, tokenFile: String, shotsDir: String) {
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
            unlink(socketPath)
            exit(0)
        }
        if pr < 0 {
            if errno == EINTR { continue }
            continue
        }
        let client = accept(fd, nil, nil)
        if client < 0 { continue }
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
      let shotsDir = parseFlag("--shots-dir") else {
    FileHandle.standardError.write("usage: jcode-computerd --socket <path> --token-file <path> --shots-dir <path>\n".data(using: .utf8)!)
    exit(2)
}
runServer(socketPath: socketPath, tokenFile: tokenFile, shotsDir: shotsDir)
