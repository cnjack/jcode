# Computer-Use Helper — Cross-Platform Native Backend Design

Status: proposed · 2026-07-16 · Extends `internal-doc/computer-use-design.md` §2.2, §9

The `computer-use` feature ships today on `FakeBackend` only. This document
designs the **real** backend: a native helper process that reads accessibility
trees, synthesizes input, and captures windows — on **macOS and Windows**, behind
one platform-neutral socket protocol.

The parent design (`computer-use-design.md`) settled the *shape* — a `Backend`
interface, a unix-socket JSON-RPC protocol (§2.2), a 9-code error taxonomy (§7).
This document settles the *how*, and the how is dominated by one fact the parent
under-weighted:

> **The two platforms disagree about almost everything except the tree.**

macOS gates automation behind per-app TCC consent and needs a stably-signed
identity to keep that consent; Windows gates almost nothing but isolates by
integrity level and session. macOS synthesizes input with `CGEventPost`; Windows
with `SendInput`. macOS reads AX over a C API; Windows reads UIA over
cross-process COM. The socket transport itself differs (unix socket vs named
pipe). **The job of this design is to draw the platform line in exactly one place
— the helper binary — so that everything above it, the entire Go stack and the
agent's mental model, never learns which OS it is on.**

---

## 0. What is already true (and must not be redesigned)

Three things are pinned by the parent design and the existing codebase. This
document builds on them; it does not revisit them.

1. **The `Backend` interface is the contract** (`internal/computer/computer.go:81`).
   Nine methods, every one taking a `context.Context` whose deadline is
   load-bearing (an unanswered permission prompt is a silent multi-minute hang,
   not an error). The helper client implements exactly these nine:

   ```go
   Kind() string
   ListApps(ctx) ([]App, error)
   Frontmost(ctx) (App, error)
   Tree(ctx, bundleID) ([]uitree.Node, error)
   Capture(ctx, bundleID) ([]byte, error)
   Launch(ctx, bundleID) error
   ReadClipboard(ctx) (string, error)
   Perform(ctx, act Action) error
   Close() error
   ```

   `FakeBackend` (`internal/computer/fake.go`) is the proof this interface is
   sufficient. `helperBackend` is "the same nine methods, but each marshals into
   a socket request instead of touching an in-memory map." If a method cannot be
   implemented cleanly over the socket, the interface is wrong — and it is far
   cheaper to find that out against the fake than against a signed daemon.

2. **The wire protocol is JSON-RPC over a length-prefixed frame** (§2.2):
   4-byte little-endian length + UTF-8 JSON, 8 MiB cap, `ping`/`request`
   methods, `apiVersion` negotiation, one request in flight at a time. This
   document keeps all of it and specifies the request/response payloads (§3).

3. **The distribution vehicle exists.** `jcode-ble` already ships as a second
   native sidecar (`desktop/src-tauri/tauri.conf.json` `externalBin`), built
   per-OS in CI, co-located next to the main binary, spawned lazily by the Go
   process via `os.Executable()`, and swept into the macOS sign+notarize pass for
   free. The helper is a third sidecar of exactly this kind. §6 details the ride.

---

## 1. The layering, and where the platform line is drawn

```
        agent tool loop            (Go, platform-agnostic)
              │
   internal/computer/  Session/Manager/tiers/approval   (Go, platform-agnostic)
              │  Backend interface — the ONLY thing above the line
              │
   ┌──────────┴───────────┐
   │  helperBackend (Go)  │   RPC client: marshals the 9 methods, dials the
   │                      │   socket, honors ctx, verifies the peer. Identical
   │                      │   on every OS — it speaks JSON, not AX or UIA.
   └──────────┬───────────┘
   ═══════════╪═══════════  ← THE PLATFORM LINE (a socket)
              │
   ┌──────────┴───────────┐        ┌──────────────────────┐
   │  jcode-computerd      │        │  jcode-computerd.exe │
   │  (Swift, macOS)       │        │  (C#/C++, Windows)   │
   │  AXUIElement          │        │  UI Automation (COM) │
   │  CGEventPost          │        │  SendInput           │
   │  ScreenCaptureKit     │        │  Windows.Graphics.   │
   │  TCC consent          │        │    Capture           │
   └──────────────────────┘        └──────────────────────┘
```

**The line is a socket, and it is drawn deliberately low.** Everything above it —
tiers, the app allowlist, uid minting, the approval integration, the frontmost
gate's *policy* — is already written, tested, and platform-neutral. The helper's
only job is to turn one JSON request into one platform call and one JSON
response. It holds no policy. It is, on purpose, the dumbest process in the
system: it does not know what a tier is, it does not decide what is allowed, it
does not mint uids. It reads a tree, it performs an action, it grabs a picture.

Why so low? Because the layer above the line is where the security lives (§4 of
the parent), and that layer must be **the same code on every platform** or the
guarantees fork. A tier check that runs in Go on macOS and in C# on Windows is
two implementations of one invariant, and they will drift. So the Go side keeps
every decision, and the helper keeps only the hands.

### 1.1 The one thing that must cross the line intact: element identity

The parent design's hardest-won correctness property (uid names an *element*, not
a position; `computer-use-design.md` §3.1) lives in `internal/uitree`, above the
line. For it to work, the helper must hand back a **stable per-element handle**
that the Go side can store in a snapshot and send back later to act on.

The two platforms offer this differently, and this is the first place the
abstraction earns its keep:

The two platforms turn out to be **structurally identical here**, which is not
what I first assumed — I expected Windows to hand back a durable id and macOS an
ephemeral pointer. The research says both are ephemeral:

- **macOS**: an `AXUIElement` is a `CFTypeRef`. It is *not* serializable and its
  validity across calls is not guaranteed once the tree mutates. So the helper
  cannot hand the Go side "the element" — it must hand back an **opaque handle it
  itself keeps a table for**: `Ref int64` → a retained `AXUIElement` in a
  per-session map, invalidated when the tree is re-read. Staleness is detected by
  the table, not by the reference: a re-read mints a fresh table, and a handle
  from an older generation is simply absent — the same "absence = stale" property
  `uitree` already relies on (parent §3.1). A dead `AXUIElement` also surfaces its
  own error on use, as a backstop.
- **Windows**: `IUIAutomationElement.GetRuntimeId()` returns an int array, but
  Microsoft's own doc says it is **only unique within the desktop session and is
  reused** once an element is destroyed — so it is emphatically *not* a durable
  handle. The Windows helper needs the **same** per-session pointer table
  (`map[int64]*IUIAutomationElement`), plus a re-locate strategy for when the Go
  side returns a handle after the tree moved: prefer `CurrentAutomationId` (a
  developer-set stable key, when the app sets one), fall back to
  `ControlType + Name + ClassName + ancestor path`, and use `GetRuntimeId` only
  as a same-session sanity check — never as the primary key. A dead handle
  surfaces as `UIA_E_ELEMENTNOTAVAILABLE`, which maps to the same "re-snapshot"
  path macOS uses.

Either way, the Go side sees only `Ref int64` — an opaque token it stores and
returns. It never learns that both platforms back it with a pointer table and a
re-locate heuristic. **This is the abstraction working: the correctness property
(`uitree` retiring a uid when its element vanishes) is one piece of Go code, and
both platforms feed it the same shape — and, as it happens, solve the underlying
problem the same way.**

---

## 2. Transport: one socket, two spellings

The parent specified a unix socket. Windows complicates this, and the resolution
is worth stating precisely because it is the most visible platform seam in the Go
code.

| | macOS / Linux | Windows |
|---|---|---|
| primary | unix socket, `~/.jcode/computer/computerd.sock`, dir mode 0700 | named pipe, `\\.\pipe\jcode-computerd-<sid>` |
| Go dial | `net.Dial("unix", path)` | `winio.DialPipe` (go-winio) |
| connection-layer guard | dir mode 0700 | **SDDL DACL on the pipe** (only the user's SID may open it) |
| app-layer peer identity | pid → `SecCode` → team id (§4) | `ImpersonateNamedPipeClient` → token SID + Authenticode (§4) |

**Windows 10 1803+ does support AF_UNIX**, and Go's `net.Dial("unix")` works on
it — tempting, because it keeps the transport literally identical. It is rejected
because **Windows's AF_UNIX has no peer-credential mechanism at all** (no
`SO_PEERCRED` equivalent; the socket-option returns nothing), so it cannot
support the peer authentication §4 requires. Named pipes can, via two mechanisms
AF_UNIX lacks: a **security descriptor set at creation time** (the DACL rejects
non-owner SIDs before a byte is exchanged) and `ImpersonateNamedPipeClient`
(a kernel-authenticated caller identity, below). go-winio is the industrial-grade
Go binding (Docker/containerd use it for exactly this). AF_UNIX-on-Windows is
also still rough (`os.ModeSocket` bit unset, `Stat` quirks) — a second reason to
prefer the mature path.

The one thing this table must **not** claim — and my first draft did — is that
named pipes make peer auth *easier* because they hand over the client pid.
`GetNamedPipeClientProcessId` exists, but it is **forgeable**: Project Zero
documented three ways to make it report an attacker-chosen pid (SMB reflection
with a crafted EA, the fixed `0xFEFF` loopback pid, and handle inheritance +
pid reuse). So the pid is a diagnostic, not a credential — see §4.

The seam is contained in one Go file behind a build tag:

```go
// transport_unix.go   //go:build !windows
func dialHelper(path string) (net.Conn, error) { return net.Dial("unix", path) }
func peerPID(c net.Conn) (int, error)          { /* LOCAL_PEERPID */ }

// transport_windows.go //go:build windows
func dialHelper(path string) (net.Conn, error) { return winio.DialPipe(path, nil) }
func peerPID(c net.Conn) (int, error)          { /* GetNamedPipeClientProcessId */ }
```

Everything else in `helperBackend` — framing, JSON, ctx handling, the request
loop — is one file, no build tags. The transport difference is four functions.

---

## 3. The protocol payloads

The envelope is the parent's `{"type": "...", "payload": {...}}` over the
length-prefixed frame. This section fills in the request/response shapes — one
per `Backend` method, because the helper is a direct RPC mirror of the interface.

### 3.1 Handshake

```
→ {"type":"ping","payload":{"clientApiVersion":"JcodeComputerIPC-1"}}
← {"type":"pong","payload":{"serverApiVersion":"JcodeComputerIPC-1","platform":"darwin","helperVersion":"1.0.0"}}
```

A version mismatch is a dedicated non-retryable error (`incompatibleClientVersion`,
-10013). The Go side learns `platform` here — not to branch on it (it must not),
but to render it in `Status` so the settings UI can say "helper: macOS, v1.0.0,
Accessibility granted".

### 3.2 The nine methods → nine request types

```
list_apps      → {apps:[{bundle_id,name,running}]}
frontmost      → {app:{bundle_id,name}}
tree           → {app,disable_diff?} → {nodes:[uitree.Node], gen}
capture        → {app} → {png_base64} | {png_ref}          (see §3.4)
launch         → {app} → {ok}
read_clipboard → {} → {text}
perform        → {action:{...Action}} → {ok} | {error:{code,message}}
```

`Action` (`computer.go:59`) crosses verbatim as the `perform` payload: `kind`,
the resolved `bundle_id` (the Go side pins this at gate time — the helper never
re-resolves an app name, closing the TOCTOU the parent §4.3 describes), `uid`
mapped to the platform handle via `ref`, and the coordinate/key/text fields.

### 3.3 `tree` — the diff lives on the Go side, deliberately

The parent (§3.1) already diffs snapshots client-side (in `Session`), because a
stateless osascript backend couldn't hold session state. That decision pays off
here: **the helper returns a full tree every time**, and the Go side diffs it.
This keeps the helper stateless per-request (simpler, more crash-tolerant) and
means the diff logic is one implementation for both platforms.

The cost is bandwidth on a large tree (Xcode's is enormous), and both platforms
have the same underlying problem — **neither exposes a "read the whole tree" call;
every attribute of every node is an individual cross-process read** — and the
same fix: batch the reads.

- macOS: `AXUIElementCopyMultipleAttributeValues` reads all the attributes of one
  node (`kAXRole`, `kAXTitle`, `kAXValue`, `kAXPosition`, `kAXSize`, `kAXEnabled`,
  the action names) in one round-trip instead of one per attribute, with
  `kAXCopyMultipleAttributeOptionStopOnError` controlling error handling. macOS
  has no whole-subtree batch, so the walk is still node-by-node, but each node is
  one call, not seven.
- Windows: `IUIAutomationCacheRequest` goes further — it can bulk-cache the same
  attributes for a whole subtree in *one* `FindAll(scope, cond, cacheRequest)`
  cross-process COM call, then everything reads from the local cache. UIA property
  reads are each a cross-process call and murderously slow uncached, so this is
  not optional.

Both stay entirely inside the helper; the Go side is unaware. The asymmetry
(macOS batches per-node, Windows can batch per-subtree) is invisible above the
line — both just return a `[]uitree.Node`.

The **`set_value` and named-action** paths are likewise a clean per-platform
mirror: macOS writes a field with `AXUIElementSetAttributeValue(elem,
kAXValueAttribute, text)` and invokes a named action via
`AXUIElementCopyActionNames` + `AXUIElementPerformAction`; Windows uses
`ValuePattern.SetValue` and the pattern interfaces (`InvokePattern`,
`TogglePattern`, …). Same two `Action` kinds (`set_value`, `menu`), two backings.

### 3.4 `capture` — by reference, not by value, when it can be

An 8 MiB frame cap (§2.2) is a soft limit for a full-window PNG. The parent's
tool layer already writes screenshots to `~/.jcode/computer/shots/<uuid>.png` and
passes an `image_ref` (`computer-use-design.md` §3.2). So the preferred path is:
the helper writes the PNG to that directory (it shares the user's filesystem) and
returns `{png_ref}`; only if that write fails does it inline `{png_base64}`. This
keeps large images off the socket entirely — the same "pass a file:// url, not
bytes" discipline codex uses.

The **coordinate-system alignment** the capture must guarantee is the subtle
part, and it is platform-specific. The *contract* is fixed regardless:
**whatever the helper captures, the coordinates it reports in the tree
(`uitree.Node` position) must be in the same space the Go side hands back for a
coordinate action.** The helper owns the transform; the Go side works in one
abstract coordinate space and never converts. The three things that must land in
that one space are: the tree's element rectangles, the synthesized-input
coordinates, and the capture's pixels.

- **Windows (settled).** All three align **only if the helper process declares
  Per-Monitor-V2 DPI awareness** —
  `SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)` at
  startup (or via manifest, which is earlier and preferred). A DPI-*unaware*
  process is silently virtualized by the OS: its UIA `BoundingRectangle`s and its
  `SendInput` coordinates come back in scaled logical units that do **not** match
  the real pixels the capture returns, producing a fixed-ratio offset on every
  click at non-96-DPI. This is the single most common Windows automation bug, and
  it is a one-line fix that must not be forgotten. `SendInput` absolute
  coordinates are the normalized 0..65535 space and **must** carry
  `MOUSEEVENTF_VIRTUALDESK` on multi-monitor setups or they map only to the
  primary display — a second fixed offense the helper owns.
- **macOS (settled).** AX positions/sizes and CGEvent coordinates are both in
  **points**; ScreenCaptureKit output is in **pixels**. The factor relating them
  is `SCContentFilter.pointPixelScale` (2.0 on Retina). So the helper's abstract
  coordinate space is **points** — the space AX and CGEvent already share, so
  input synthesis needs no conversion at all — and only the capture path
  converts: it sets `SCStreamConfiguration.width/height` to
  `contentRect × pointPixelScale` to grab a full-resolution image, but reports
  every tree rectangle to the Go side in points. This is cleaner than Windows,
  where all three spaces are pixels and the burden is instead *declaring* the DPI
  mode so the OS stops virtualizing them. Same contract, opposite chore: macOS
  converts the capture, Windows converts nothing but must opt out of scaling.

---

## 4. Peer authentication — the security boundary, per platform

A unix socket (or named pipe) is reachable by any process of the same uid. A
token in a 0600 file (the `tokens.go` `StableToken` pattern, reusable as a
*belt*) is readable by any process of the same uid too. So the actual boundary,
as the parent §2.2 insists, is **verifying the peer's code identity** — and this
is the second place the platforms diverge hard.

**macOS** — pid → SecCode → team identifier, with a token as the real boundary:
1. `getsockopt(fd, SOL_LOCAL, LOCAL_PEERPID)` for the peer pid.
2. `SecCodeCopyGuestWithAttributes` with `kSecGuestAttributePid` → the peer's
   `SecCode`.
3. `SecCodeCheckValidity` against a designated requirement pinning jcode's team
   identifier → confirm the peer is our binary.

The research resolved the open question here, and the answer forces a design
choice: **a bare unix socket cannot obtain an audit token.** The audit token —
which carries a `p_idversion` field that makes it immune to pid reuse — is a
property of XPC/Mach messages, exposed via `xpc_connection_get_audit_token`, not
of a plain socket. So steps 1–3 above have a genuine **pid-reuse TOCTOU**: between
reading the pid and checking the signature, that pid can be recycled to an
attacker's process (this is a documented XPC attack class, and it applies a
fortiori to a socket with no audit token at all).

Two ways to close it, and the design picks the second:

- **Switch macOS to XPC.** XPC gets the audit token, and macOS 13+ has
  `NSXPCConnection.setCodeSigningRequirement` — the strongest possible peer auth.
  But XPC services are launchd-managed Mach services, which collides with the
  lazy-spawn lifecycle (§5): the Go process could no longer own the daemon's
  lifetime, and the macOS transport would diverge entirely from Windows's pipe.
  Rejected — the security gain does not justify forking the lifecycle model and
  the transport story.
- **A first-frame one-time token, as the actual boundary — not a belt.** The Go
  process writes a fresh random token to a 0600 file (the `tokens.go`
  `StableToken`/`IssueToken` pattern, reused exactly) and the daemon requires it
  in the connection's first frame. **This is immune to pid reuse by
  construction**: even an attacker who wins the pid race does not possess the
  token, which lived only in a 0600 file and the two legitimate processes. The
  SecCode check (steps 1–3) stays as defense-in-depth, but the token is what
  actually holds the line.

This reframes the parent §2.2's "token as belt, pid+sig as suspenders": on a bare
socket the **token is the suspenders** and the pid+sig check is the belt, because
only the token survives pid reuse. The same reasoning applies to Windows, where
`GetNamedPipeClientProcessId` is likewise forgeable (§4) — so the first-frame
token is the one peer-auth mechanism that is sound and identical on both
platforms, and the platform-specific SID/SecCode checks are the hardening on top.

**Windows** — two layers, because the obvious one-layer answer (trust the pid) is
forgeable. My first draft got this exactly backwards, calling Windows
"structurally safer here" on the theory the pid comes without a race. It does
not: `GetNamedPipeClientProcessId` can be made to report an attacker-chosen pid
(§2). So:

1. **Connection layer — SDDL DACL.** The pipe is created with a security
   descriptor (`winio.PipeConfig{SecurityDescriptor}`) that grants open access
   only to the current user's SID. A different-user process cannot connect at
   all; this is the belt, applied before any byte is read.
2. **Application layer — impersonated token, not pid.**
   `ImpersonateNamedPipeClient` makes the server thread briefly assume the
   client's security context; `OpenThreadToken` + `GetTokenInformation(TokenUser)`
   then yields a **kernel-authenticated SID** bound to this connection's logon
   session — an identity that cannot be spoofed the way the pid can — then
   `RevertToSelf`. Optionally `QueryFullProcessImageName` + `WinVerifyTrust` on
   the client binary *once Windows signing exists*, but the SID check is the
   load-bearing one and does not depend on signing.

The pid (`GetNamedPipeClientProcessId`) is kept only as a diagnostic/log value,
never as the credential. The genuine platform asymmetry is in the *hardening*,
not the boundary: the boundary is the first-frame token on both sides (§4,
macOS), and on top of it macOS adds a SecCode/team-id check and Windows an
impersonated-SID check. Neither platform's pid is trusted. The abstraction hides
this — `peerVerified(conn) bool` is one function on each side — but the two
implementations are not mirror images, and pretending they were is how the
Windows side would have shipped a forgeable pid check as if it were sound.

Both back the parent's `senderProcessNotAuthenticated` (-10000) /
`couldNotGetSenderPID` (-10017) codes. The helper serves no request until the
peer passes. **Windows signing (§6) strengthens layer 2 but is not a prerequisite
for it** — the SID check works on an unsigned binary; signing only adds the
"and it's *our* binary, not just some process of the right user" refinement.

---

### 4.1 The permission-gate asymmetry (and why first-run UX forks)

The single biggest platform difference the research found is not an API — it is
whether the OS gates UI automation *at all*.

- **macOS gates hard.** AXUIElement, CGEventPost, and ScreenCaptureKit each
  require a TCC grant the user must toggle by hand in System Settings, and the
  grant is tied to code identity. A first run *cannot* proceed until the user
  visits the Accessibility and Screen Recording panels. The settings UI's
  "which gate is shut" story (parent §6.1) is load-bearing here.
- **Windows barely gates.** UIA, `SendInput`, and unpackaged Windows.Graphics.
  Capture all work from any ordinary user process, at the same or lower integrity
  level, with **no per-app consent panel and no signature requirement**. The only
  gate is integrity level (UIPI): you cannot automate a window running at *higher*
  integrity (an elevated/admin app) without `uiAccess="true"` — which *does*
  require signing + install in a protected dir, but is a v1 non-goal.

This is a rare case where a platform difference is a *simplification* to exploit,
not a cost to abstract over: **the Windows helper has no first-run authorization
flow at all.** So the abstraction cannot be "one permission state machine for
both" — the honest shape is a `Permissions(ctx) → {granted, blocker, prompt_url}`
call that on macOS reflects real TCC state and on Windows is a near-constant
"granted, nothing to do." The settings UI branches on `blocker`, which is simply
always empty on Windows. Forcing macOS's grant-flow ceremony onto Windows would
invent a dialog the OS never asked for.

Two Windows-specific gotchas the gate-free story hides, both belonging to the
*action* path, not the *permission* path:

- **UIPI silent failure.** When `SendInput` is blocked by UIPI (target window is
  higher-integrity), it does **not** error — it returns 0 events inserted and
  `GetLastError` does not say why. So the helper cannot rely on the API to report
  a refused action; it must read back UIA state after acting to confirm the
  action landed. This feeds the parent's post-action verification, and it is the
  Windows analog of macOS's "the AX tree may be incomplete, fall back to a
  screenshot" honesty.
- **Integrity boundary at the frontmost check.** The parent's "re-confirm the
  frontmost app before every action" gate (§4.3) must, on Windows, also carry the
  awareness that a *higher-integrity* frontmost window will silently eat input.
  The gate's policy is unchanged; the helper's report of "did it work" is what
  gets the extra check.

## 5. Process lifecycle

Copied from `jcode-ble`'s spawn model (`ble_nocgo.go`) for binary resolution, and
from `browser/manager.go`'s `getManaged` for the lazy-singleton-with-liveness
half. The synthesis:

1. **Resolve.** `os.Executable()` → sibling `jcode-computerd[.exe]`, with a
   `$JCODE_COMPUTERD` env override (the desktop shell can point at a bundled
   copy) and a glob fallback for the dev-mode target-triple suffix — the exact
   three-tier resolution `ble_nocgo.go:helperPath()` already uses.
2. **Lazy start.** Nothing spawns at process startup. The daemon starts the first
   time `computer-use` is enabled *and* a session opens — so a user who never
   touches computer use never triggers a TCC prompt, and the machine that has it
   disabled never runs a native automation daemon. This mirrors BLE's
   config-toggled spawn precisely, and it is a security property, not just tidiness.
3. **Reuse + liveness.** The daemon is long-lived (unlike BLE's per-toggle
   process) because a TCC prompt should happen once, not once per task. `Manager`
   holds the connection; each `OpenSession` pings it; a dead daemon is torn down
   and re-spawned, exactly `getManaged`'s `alive()` loop.
4. **Teardown.** `Manager.Close()` closes the socket and signals the daemon to
   exit. The daemon also self-exits after an idle timeout, so a crashed jcode
   never leaves an automation daemon running.

The three "not implemented" returns in `manager.go` (`:160` helper, `:162` osa,
`:168` auto) are the exact lines this replaces.

**The daemon must be a user-session process on both platforms — and on Windows
this is a hard OS constraint, not a preference.** A Windows Service runs in
Session 0, which since Vista is isolated from every interactive desktop: it has
no access to `WinSta0\Default`, so UIA cannot read the user's app trees and
`SendInput` has no interactive desktop to target. The one historical bridge
(Interactive Services Detection / `UI0Detect`) was **removed in Windows 10 1803**
and later, so there is no Session-0-to-user-desktop path at all anymore. macOS is
the same shape for a softer reason: this is a **LaunchAgent** (per-user session),
never a LaunchDaemon (system-wide), because automation belongs to the logged-in
user's session. The lazy-spawn model (2) satisfies both naturally — the Go
process is itself a user-session process, and a child it spawns inherits that
session. If persistent auto-start is ever wanted, the per-platform equivalents
are a LaunchAgent plist (macOS) and a per-user Scheduled Task with an "at log on"
trigger (Windows) — *not* a Windows Service, ever.

### 5.1 What happened to `osaBackend`?

The parent (§2.1, §10.1) proposed an osascript backend as a no-build-toolchain
fallback that ships before the signed helper. This design **de-prioritizes it**,
on evidence: the parent's own §10.1 probe found System Events timing out even for
the trivial "who is frontmost" query, and while that was a sandboxed probe (not
conclusive), AppleScript's `entire contents` on a real Xcode window is a known
multi-second-to-hang operation. Building the helper as the *primary* path on both
platforms, with the fake as the only fallback, is cleaner than shipping a
degraded osa backend that works on Calculator and dies on anything real. If a
no-signing dev path is needed on macOS, an **ad-hoc-signed local build of the
Swift helper** (research confirms `swiftc` is present) is a better fallback than
osascript — it exercises the real code path, just without a stable TCC identity.

---

## 6. Build & distribution

Rides the `jcode-ble` precedent, with one new platform cost.

**macOS** — nearly free, and the Developer ID signature is not just a
distribution nicety here: **it is what makes TCC consent survive updates**, which
is the whole reason the native code lives in a signed bundle and not in the Go
binary (parent design C2). Research confirms the mechanism: a TCC grant is
matched against the code signature's *designated requirement*, which pins the
**Team ID**; a Developer-ID-signed bundle keeps a stable Team ID across releases,
so Accessibility/Screen-Recording consent granted once persists forever. An
ad-hoc-signed or unsigned binary is identified only by its CDHash, which changes
on **every** `go build` — so it would re-prompt for consent on every update, and
a tool that asks for Accessibility permission every time is a tool nobody keeps
enabled. This is the concrete evidence under C2's claim; it is not a preference.

Add `jcode-computerd` as a third `externalBin`
(`tauri.conf.json:44`); build it in a new CI step (native macOS runner, `swiftc`)
alongside the existing sidecar builds (`release.yml:260`); it is swept into the
single `pnpm tauri build` sign+notarize pass under the same Developer ID. The one
addition: **Accessibility and Screen Recording usage-description strings in
`Info.plist`** — and this is a known trap, not a hypothetical: the desktop app
already hit a SIGABRT-on-launch bug from a *missing* Bluetooth usage description
(memory: `jcode-desktop-app`). The same failure mode awaits a missing
`NSAccessibilityUsageDescription` / screen-capture description.

**Windows** — cheaper than my first draft feared, because the research corrected
two assumptions. There is **no Windows signing infrastructure today**
(`release.yml`'s Windows matrix has no `signtool`), but signing turns out **not**
to be a prerequisite for the helper to *function*: UIA, `SendInput`, and
unpackaged Windows.Graphics.Capture need no signature at all (§4.1), and peer auth
leans on the impersonated SID, not on the client's signature (§4). So an unsigned
Windows helper is fully functional and its peer auth is sound.

Signing is still **wanted**, for two softer reasons:
- SmartScreen reputation — an unsigned binary that synthesizes input draws more
  friction on first launch.
- It upgrades peer-auth layer 2 from "a process of the right user" to "*our*
  binary of the right user."

So Windows signing is scheduled (§7 phase 5) as a distribution-quality gate, not
a functional blocker. The Windows helper can develop and even ship for local use
before it exists, with the SmartScreen friction and the slightly weaker peer-auth
refinement logged honestly — the same posture the parent takes on every other
"shipped but degraded" state. (The one place signing *is* an API prerequisite —
`uiAccess="true"` to automate elevated windows — is a v1 non-goal, §4.1.)

**CLI-only users** (who install the plain `jcode-<os>-<arch>` binary, not the
desktop app) get no helper from this path. A separate download-on-enable step
(the settings UI already has an "Install helper" button per
`computer-use-design.md` §6.1) fetches the signed helper for them. Out of scope
for v1; noted so the settings-UI button is not mistaken for already wired.

---

## 7. Phasing

The order is chosen so each phase is independently testable and the riskiest
platform assumption is measured before the most code is written.

1. **Protocol + `helperBackend` (Go), against a mock daemon.** No native code
   yet — a throwaway Go "daemon" that serves canned trees over the real socket,
   so the client, the framing, the ctx handling, and the peer-auth plumbing are
   all exercised and unit-tested before a line of Swift exists. This is the
   `FakeBackend`-over-a-real-socket step, and it de-risks the entire Go half.
2. **macOS helper, ad-hoc signed.** The real Swift daemon: AX tree, CGEvent,
   ScreenCaptureKit, TCC. Ad-hoc signed, run locally, no distribution — proves
   the platform APIs work end-to-end. **This is where the highest-risk unknowns
   resolve** (AX perf on a real tree, coordinate alignment, auto-wait), so it
   comes before any packaging investment.
3. **macOS distribution.** Developer ID + notarization via the existing Tauri
   pipeline; the `Info.plist` strings; the settings-UI install button.
4. **Windows helper.** UIA + SendInput + Graphics.Capture, over a named pipe.
   Independently, in parallel with (3) if there's a second pair of hands — the
   protocol is frozen after phase 1, so the two platforms cannot drift.
5. **Windows signing + distribution.** The prerequisite from §6. Gates the
   Windows helper's public release, not its development.

Phase 1 is the keystone: freezing the protocol against a mock is what lets macOS
and Windows be built by different people at different times without a shared line
of native code, and it is pure Go — no signing, no TCC, no COM, testable in CI.

---

## 8. Security posture, restated for the native layer

The parent's §4 security model is unchanged and unmoved — it all lives above the
line. What the native layer adds is a small, sharp set of its own obligations:

- **The helper holds no policy.** It cannot be tricked into escalating because it
  makes no decisions. Every "may I" is answered in Go before the request is sent.
  A compromised helper can do what the OS lets the user's session do — but so can
  any process the user runs; the helper adds no privilege the tier system hasn't
  already gated above it.
- **Peer auth is the helper's one guard** (§4): it serves only jcode. This is
  what stops a *different* same-uid process from using the running daemon as a
  confused deputy to drive the screen.
- **Lazy spawn is a security property** (§5.2): no automation daemon exists until
  the user turns the feature on, so the attack surface is absent by default, not
  merely dormant.
- **Idle self-exit** bounds the window: a daemon does not outlive the work.
- The `userIntervened` / `screenLocked` kill switches (parent §7) are enforced by
  the helper because only it can see the live input state — these are the one
  place the helper makes a "stop" decision, and it is a fail-safe one (stop), not
  a fail-open one.

---

## 9. Research findings — the three open questions, resolved

All three are answered; the answers are folded into the sections above and
summarized here.

1. **macOS AX handle stability + snapshot performance** (§1.1, §3.3). Resolved:
   a per-session element table with explicit invalidation *is* required —
   `AXUIElement` is an ephemeral `CFTypeRef`, and (the surprise) Windows's
   `GetRuntimeId` is *also* documented as session-only and reused, so **both
   platforms need the same handle-table + best-effort re-locate strategy**. For
   perf, neither platform exposes a whole-tree read; macOS batches per-node with
   `AXUIElementCopyMultipleAttributeValues`, Windows per-subtree with
   `IUIAutomationCacheRequest`. Same shape above the line.
2. **macOS peer auth over a bare socket** (§4). Resolved, and it changed the
   design: a bare unix socket **cannot** get an audit token (that is an
   XPC/Mach-message property), so the pid+SecCode check has a real pid-reuse
   TOCTOU. Rather than switch macOS to XPC (which would fork the lifecycle and
   transport), the **first-frame one-time token becomes the actual boundary** —
   immune to pid reuse by construction, and identical on both platforms (Windows's
   pid is likewise forgeable). The platform-specific SecCode/SID checks are
   hardening on top.
3. **Coordinate-system alignment** (§3.4). Resolved both platforms: macOS works
   in points (AX and CGEvent already share them) and converts only the capture
   via `SCContentFilter.pointPixelScale`; Windows works in pixels and instead must
   declare `PER_MONITOR_AWARE_V2` so the OS stops virtualizing, plus
   `MOUSEEVENTF_VIRTUALDESK` for multi-monitor.

Two cross-platform confirmations fell out of the research, both validating parent
claims rather than opening new questions:

- **Input delivery goes to the focused app, carrying no target, on both
  platforms.** macOS `CGEventPost(kCGHIDEventTap, …)` posts into the system HID
  stream and lands on whatever holds focus; Windows `SendInput` is explicitly
  documented as serialized into the input stream with no target-window parameter.
  This is the hard confirmation of the parent's §4.3 thesis: the coordinate
  carries no identity, so the frontmost check *must* run at action time — it is
  forced by both OSes' input models, not a design preference.
- **Auto-wait is retry-until-settled, not event subscription.** The pragmatic
  approach (confirmed against a cross-platform automation library) is a lazily
  evaluated locator that retries for a short window to let the UI settle, rather
  than subscribing to `AXObserver`/`kAXValueChangedNotification`. The helper
  implements the parent's "auto-wait ~1s, up to 5s under a loading indicator"
  (parent §2) this way: after an action, re-read and retry briefly before
  returning, on both platforms. This doubles as the Windows post-action
  verification the UIPI silent-failure problem (§4.1) requires — the same
  re-read serves both needs.

### 9.1 A note on how this research was obtained

The two macOS research agents dispatched for this (AX/input/capture; TCC/peer-auth)
**wedged** — 27 minutes of silence mid-run, one having made zero WebFetch calls.
The findings above were gathered by direct targeted search instead, prioritizing
the two questions that actually gated architecture (TCC stable-identity, which
underpins C2; and the audit-token question, which decided socket-vs-XPC). This is
noted so a reader does not assume a full macOS research sweep happened — the
depth here is "enough to settle the load-bearing decisions," not "exhaustive."
Remaining lower-stakes details (exact `AXObserver` timing constants, the full
`kAXRole` → `uitree` role table) are left to implementation phase 2 (§7), where
they are cheap to pin against the real API.
</content>
