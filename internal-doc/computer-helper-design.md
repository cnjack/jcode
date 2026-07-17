# Computer-Use Helper — macOS Native Backend Design

Status: **partially implemented (macOS)** · 2026-07-16 · Extends
`internal-doc/computer-use-design.md` §2.2, §9

> **Implementation status.** Phase 1 (Go protocol + `helperBackend` + protocol tests)
> and most of phase 2 (the macOS Swift daemon) are **built and tested** — see §11
> for the per-requirement matrix. The Go client is fully unit-tested against a
> mock; the real Swift daemon is proven over a real socket for the no-TCC paths;
> the AX/CGEvent/SCK paths compile and return correct errors without a grant but
> are not exercised under a real TCC grant (that needs a manual grant this
> environment can't automate). The shipping product is intentionally macOS 14+
> only; Windows/Linux are not exposed as partially supported platforms.

The `computer-use` feature ships with one real backend: native Swift helper
processes that read accessibility trees, synthesize input, and capture windows
on macOS 14+. Deterministic fakes exist only in unit tests and explicit
`jcode_eval` builds; there is no production mock or backend selector.

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

   `FakeBackend` (`internal/computer/fake.go`) is the test proof this interface is
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
   ┌───────────────────────┐
   │  jcode-computerd       │
   │  (Swift, macOS 14+)    │
   │  AXUIElement           │
   │  CGEventPost           │
   │  ScreenCaptureKit      │
   │  TCC consent           │
   └───────────────────────┘
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
  cannot hand the Go side "the element" — it hands back an **opaque `Ref int64`
  backed by a per-session table keyed on element identity** (`CFEqual`/`CFHash`).
  This detail is load-bearing, and my first draft got it wrong: it said the table
  was *rebuilt fresh each snapshot*. That would churn every uid on every snapshot
  and defeat stale-uid detection, because uitree above the line uses the Ref *as*
  the element's identity. The table must instead **persist**: the same element
  seen in two snapshots gets the same Ref, and a departed element keeps its Ref
  reserved, never reissued. That is exactly what makes uitree's "same element
  keeps its uid, departed element's uid retires" hold across the line. A dead
  `AXUIElement` surfaces its own error on use, as a backstop. (Built as
  `ElementRegistry` in the daemon; caught during implementation, §11.)
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
| primary | per-process-instance unix socket, `~/.jcode/computer/computerd-<nonce>.sock`, dir mode 0700 | named pipe, `\\.\pipe\jcode-computerd-<sid>` |
| Go dial | `net.Dial("unix", path)` | `winio.DialPipe` (go-winio) |
| connection-layer guard | dir mode 0700 | **SDDL DACL on the pipe** (only the user's SID may open it) |
| app-layer peer identity | current: peer PID + token; planned: signed parent / audit token (§4) | `ImpersonateNamedPipeClient` → token SID + Authenticode (§4) |

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

### 3.4 `capture` — by reference across IPC, by value into model vision

The 8 MiB frame cap (§2.2) applies to protocol JSON, not to the PNG. The capture
worker writes the PNG atomically in the per-process handoff directory and the
daemon returns `{png_ref}`. Go opens that exact regular file without following a
symlink, enforces a 20 MiB limit and a PNG signature, reads it, and removes the
handoff copy. This keeps binary media off the socket while still allowing the
tool layer to attach the actual bytes as an Eino `image/png` result. A separate
mode-0600 UI copy is addressed by `image_ref`; that local URL is for rendering
and is not mistaken for model vision.

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
  converts: it uses a desktop-independent single-window filter, removes window
  shadows, scales up to `pointPixelScale`, and caps the long edge at 2048 pixels.
  Metadata reports the actual returned `CGImage` dimensions rather than an
  assumed scale. Every tree rectangle still reaches Go in points. This is cleaner than Windows,
  where all three spaces are pixels and the burden is instead *declaring* the DPI
  mode so the OS stops virtualizing them. Same contract, opposite chore: macOS
  converts the capture, Windows converts nothing but must opt out of scaling.

---

## 4. Peer authentication — the security boundary, per platform

A unix socket (or named pipe) is reachable by any process of the same uid. A
token in a 0600 file is also readable by another process running as that uid, so
file mode alone is not a same-uid security boundary. The current macOS daemon
therefore combines two checks: the kernel-reported peer PID must equal the jcode
PID passed at spawn time, and the first frame must carry the token. This prevents
a different live PID from borrowing that *already-running daemon instance*. It
is not a same-uid trust boundary: another process can execute the TCC-authorized
helper binary itself with its own PID, token and socket. Nor does it give the Go
client a cryptographic identity for the server. Signed-parent validation plus an
inherited socket, or XPC audit-token identity, is required for that stronger
claim.

**macOS hardening path** — pid → SecCode → team identifier, on top of PID+token:
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

Two stronger mutual-identity designs remain; neither is claimed by the current
implementation:

- **Switch macOS to XPC.** XPC gets the audit token, and macOS 13+ has
  `NSXPCConnection.setCodeSigningRequirement` — the strongest possible peer auth.
  But XPC services are launchd-managed Mach services, which collides with the
  lazy-spawn lifecycle (§5): the Go process could no longer own the daemon's
  lifetime, and the macOS transport would diverge entirely from Windows's pipe.
  Rejected — the security gain does not justify forking the lifecycle model and
  the transport story.
- **An inherited socket/socketpair or XPC mutual identity channel.** This would
  remove the filesystem rendezvous race and let both sides verify whom they are
  speaking to. Until then, PID+token is a practical containment improvement, not
  a claim of signed mutual authentication.

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
   never leaves an automation daemon running. Its process-instance screenshot
   handoff (`handoff-PID-<128-bit nonce>`) is cleared on dial/reconnect and normal
   close; the daemon also removes its own directory on startup/idle exit. It
   strictly parses and sweeps dead nonce siblings, while legacy `handoff-PID`
   migration entries require an age grace and a repeated liveness check.

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

### 5.1 What happened to `osaBackend` and the backend selector?

The parent (§2.1, §10.1) proposed an osascript backend as a no-build-toolchain
fallback that ships before the signed helper. This design **de-prioritizes it**,
on evidence: the parent's own §10.1 probe found System Events timing out even for
the trivial "who is frontmost" query, and while that was a sandboxed probe (not
conclusive), AppleScript's `entire contents` on a real Xcode window is a known
multi-second-to-hang operation. Building the helper as the only production path
is cleaner than shipping a degraded osa backend that works on Calculator and
dies on anything real. If a
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

`tauri.macos.conf.json` replaces the base `externalBin` array with all four
macOS binaries: `jcode`, `jcode-ble`, the AX daemon, and the isolated capture
worker. The release job compiles both Swift helpers with an explicit macOS 14
deployment target before the existing `pnpm tauri build` sign/notarize pass.
Keeping this in a platform config is load-bearing: Windows and Linux must not be
asked for helpers they cannot build.

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

**CLI-only users** receive matching daemon and capture assets in every macOS
release. `script/install.sh` downloads and SHA-256-verifies all three binaries
before installing any of them; `make install` compiles both helpers into the same
Go bin directory as `jcode`. Runtime discovery therefore remains the same exact
sibling lookup for local, CLI-release, and Tauri installs.

---

## 7. Delivery status

1. **Protocol + `helperBackend`: delivered.** Framing, token auth, deadlines,
   reconnect, and mutation non-replay are covered against a mock daemon.
2. **macOS helpers: delivered and live-tested.** The AX daemon plus isolated
   ScreenCaptureKit worker drive Calculator end to end under real TCC grants.
3. **macOS distribution: delivered.** Local builds, `make install`, CLI release
   assets, the shell installer, and Tauri bundles all ship the same pair with a
   macOS 14 deployment target. Developer-ID signing/notarization is applied by
   the existing release job when its optional credentials are configured.
4. **Windows helper and signing: deferred.** UIA, SendInput, named-pipe transport,
   and its distribution remain future platform work.

---

## 8. Security posture, restated for the native layer

The parent's §4 security model is unchanged and unmoved — it all lives above the
line. What the native layer adds is a small, sharp set of its own obligations:

- **The helper holds no policy.** It cannot be tricked into escalating because it
  makes no decisions. Every "may I" is answered in Go before the request is sent.
  A compromised helper can do what the OS lets the user's session do — but so can
  any process the user runs; the helper adds no privilege the tier system hasn't
  already gated above it.
- **Instance admission is the helper's one local guard** (§4): PID+token stops a
  different live process from using the already-running daemon as a confused
  deputy. It does not authenticate who launched a new helper instance, so this
  is containment against accidental/cross-process borrowing, not signed local
  process identity.
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
   TOCTOU. The first-frame random token prevents accidental/stale rendezvous and
   binds a connection to one launch, but a mode-0600 file is readable by the same
   uid and therefore is not an adversarial same-uid boundary. A signed-parent +
   inherited-socket design, or XPC audit tokens, is the remaining hard boundary.
   The platform-specific SecCode/SID checks are useful hardening on top.
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

---

## 10. First-run onboarding — the branded permission ceremony (2026-07-17)

Bare TCC prompts put *binary names* in System Settings: one row for
`jcode-computerd` (Accessibility) and another for `jcode-computerd-capture`
(Screen Recording), each with a generic icon. The fix has two halves, both
riding the same insight: **the .app bundle is the unit of TCC identity.**

1. **One bundle, one identity, one icon.** `jcode-computerd.app` ("jcode
   Computer Use", `com.cnjack.jcode.computerd`) holds all three helper
   executables — daemon, capture worker, onboarding UI — so both grants land
   on a single branded row. Exactly one helper identity, deliberately: the
   main jcode app and the helper are separate (a prompt fired from jcode would
   attribute to jcode/Terminal instead), but the helpers never split further —
   every extra bundle would be another row the user has to authorize. This is
   the Codex "Codex" / "Codex Computer Use" shape.

2. **The ceremony runs under the helper's identity.** The onboarding UI
   (`cmd/jcode-computerd/onboarding`, Rust + AppKit via objc2) is a third
   executable inside the bundle, spawned by the daemon with the same
   disclaimed-responsibility SPI as the capture worker, so the TCC calls its
   Allow buttons fire are attributed to the bundle. Two windows:

   - **Dialog** — "Enable jcode Computer Use": icon, one line of why, and an
     Allow row per grant (Accessibility, Screen Recording). Allow fires the
     real consent prompt *and* deep-links the exact Settings pane; rows flip
     to a green check as the poll (0.5 s) observes grants; when both are held
     the window dismisses itself.
   - **Drag bar** — a floating panel that decides *whether to exist* and
     *where* from the System Settings window's position: shown only while
     Accessibility is missing **and** System Settings is the frontmost app
     with a window on screen (polled via `CGWindowListCopyWindowInfo` +
     `NSWorkspace.frontmostApplication`, both zero-grant APIs — no
     chicken-and-egg; the frontmost check keeps the bar from hovering over
     unrelated work while Settings sits buried), re-anchored to the window's
     bottom edge every tick, hidden when Settings loses focus or the grant
     lands. The chip inside is an `NSDraggingSource` carrying the .app's file
     URL, for the previously-denied case where macOS will not re-prompt and
     dragging the app into the list is the only path.

   The daemon surfaces it from three places — the `request_permissions` RPC
   and both once-per-launch auto-prompt paths — via `surfaceOnboardingUI()`,
   which is **bundle-gated**: bare-binary runs (dev builds, unit tests) have
   no bundle identity worth priming and keep the old direct prompts. A
   same-uid flock (`$TMPDIR/jcode-computerd-onboarding.lock`) keeps the
   ceremony single-instance across daemons; the daemon additionally reuses a
   still-running child instead of respawning.

   The helper's **icon is drawn in code** (`--render-icon`, tiny-skia): a
   brand-orange gradient tile with a white cursor arrow and the main icon's
   pixel-square motif in white — family-recognizable, unmistakably not the
   jcode app icon. `script/render_computerd_icon.sh` regenerates the committed
   `.icns`; bundle builds just copy it. Dev modes: `--state` (grant JSON),
   `--demo` (fresh-user layout, no auto-exit), `--demo-shot <dir>` (renders
   both windows to PNG via `cacheDisplayInRect` — our own view hierarchy, no
   Screen Recording needed).

   Resolution order is the same three-tier lookup as the other helpers:
   `$JCODE_COMPUTERD_ONBOARDING` override → suffixed sibling → bare sibling.
   On the Go side, `helperBinPath` prefers the `.app` bundle daemon over the
   bare binary, and the dev-glob skips `-capture`/`-onboarding` siblings.

   One accidental-but-valuable confirmation from testing: launched *without*
   the disclaim (plain `./…-onboarding` from a terminal), the UI reported both
   grants as already held — it had inherited the terminal's responsible-
   process identity, which is precisely the mis-attribution the disclaimed
   spawn (and the bundle) exists to prevent. The E2E test
   (`TestSmokeBundleOnboardingSpawn`) drives the real RPC against the bundled
   daemon and asserts the UI child appears.

   **The daemon disclaims itself.** The adversarial review caught the
   critical inverse of that accident: the *daemon* is spawned by jcode with a
   plain fork/exec, so its own `AXIsProcessTrusted` would key on jcode's
   responsible process (Terminal/desktop app) — the ceremony would flip the
   "jcode Computer Use" row while `requireAccessibilityTrusted` kept
   consulting Terminal's. So a bundle-resident daemon re-execs itself once
   through the same disclaim SPI at startup (`maybeReexecSelfResponsible`,
   env-marker guarded); the original process lingers as a signal-forwarding
   supervisor so the Go parent's process handle still reaches the real
   daemon. Bundle residency is verified against the bundle's Info.plist
   identifier, not path shape (Tauri ships bare sidecars under
   `jcode.app/Contents/MacOS/`, which must not count), and the
   `JCODE_COMPUTERD_ONBOARDING` override is honored only from inside the same
   bundle — an out-of-bundle UI would prime a throwaway identity while the
   ceremony claims success. All three executables are signed with the
   bundle's identifier so that, under Developer ID, a grant recorded from one
   validates for the others; ad-hoc dev builds remain pinned per-binary by
   cdhash (known dev-mode re-prompt limitation, same as identity churn per
   rebuild).

---

## 11. Implementation status (2026-07-16)

Per-requirement, so a reader knows exactly what runs and what is still design.

| Requirement | §ref | Status | Where |
|---|---|---|---|
| Wire protocol (framing, envelope, handshake, error taxonomy) | §2,§3 | ✅ built + unit-tested | `internal/computer/proto.go` |
| `helperBackend` — 9 methods, one-in-flight, ctx honor, token auth | §1,§3,§4 | ✅ built + unit-tested | `internal/computer/helper.go` |
| dial / lazy-spawn / cache-reuse | §5 | ✅ built (macOS) | `internal/computer/helper_dial.go` |
| mock daemon over net.Pipe (full client coverage) | §7.1 | ✅ | `internal/computer/helper_test.go` |
| **Real Go↔Swift integration over a socket** | §7.2 | ✅ tested (no-TCC paths) | `internal/computer/helper_smoke_test.go` |
| Swift daemon: NSWorkspace apps/frontmost/launch, clipboard | §3.2 | ✅ runs (no TCC needed) | `cmd/jcode-computerd/main.swift` |
| Swift daemon: AXUIElement tree, ref actions, CGEvent | §3.2 | ✅ real Calculator E2E under Accessibility grant | `cmd/jcode-computerd/main.swift`, `helper_calculator_e2e_test.go` |
| Swift worker: ScreenCaptureKit window capture | §3.4 | ✅ real PNG + daemon-survival E2E | `WindowCaptureHelper.swift`, `helper_calculator_e2e_test.go` |
| **Element Ref stable across snapshots** (element→ref table) | §1.1,§9.1 | ✅ built + exercised end to end | `ElementRegistry` in daemon |
| Per-attribute AX reads with bounded traversal | §3.3 | ✅ built; batch optimization deferred | `axValue`, `TreeBuilder` in daemon |
| **Auto-wait** (settle after an action) | §7,§9 | ✅ built (fixed settle; loading-indicator extend deferred) | `settleUI` in daemon |
| **Idle self-exit** | §5,§8 | ✅ built + tested | daemon accept loop; `TestSmokeDaemonIdleExit` |
| Process-instance screenshot handoff cleanup | §3.4,§5 | ✅ nonce socket/handoff + dial/close + legacy-aware daemon sweep; real idle-exit smoke | daemon lifecycle; `TestHelperHandoffCleanupIsProcessScoped` |
| **screenLocked kill switch** | §8 | ✅ built; ⚠ lock-screen path not automatable in test | `checkScreenUnlocked` in daemon |
| tree diff on the Go side | §3.3 | ✅ (pre-existing) | `Session.Snapshot` |
| coordinate contract (window points + PNG pixel mapping) | §3.4 | ✅ built + real E2E | daemon/worker metadata + screenshot tool text |
| Peer auth: expected client PID + first-frame token | §4 | ✅ built; real happy path + bad-token unit coverage | daemon + client |
| Point-of-need TCC requests (`request_permissions` RPC, worker `--request-permission`, once-per-launch auto-prompt on grant failure) | §4.1 | ✅ built + live-smoke-tested (real socket round-trip) | `requestAccessibilityPermission`/`requestCaptureWorkerPermission` in daemon, `helperBackend.RequestPermissions`, `Manager.RequestPermissions`, `POST /api/computer/permissions`, `/computer grant` |
| Peer auth: SecCode/team-id hardening on top of token | §4 | ⬜ deferred | — |
| liveness / reconnect of a crashed daemon | §5 | ✅ reconnect once on next request; mutations never replayed | `helper.go`, `helper_test.go` |
| macOS packaging + deployment target | §6–7 | ✅ CLI + installer + Tauri; macOS 14 min | `Makefile`, `release.yml`, `install.sh` |
| Developer ID signing + notarization | §6–7 | ✅ existing optional release path covers bundled helpers | `release.yml` |
| Non-macOS product gate | all | ✅ status-only explanation; no tools or enablement | `internal/computer/platform.go`, command/web composition |
| **One-identity .app bundle** (daemon + capture + onboarding, own icon) | §10 | ✅ built; `make build-computerd-bundle`; dial prefers bundle | `script/build_computerd_bundle.sh`, `helper_dial.go` |
| **Daemon self-responsibility** (disclaim re-exec, supervisor lingers) | §10 | ✅ built + tree/signal-forwarding verified live | `maybeReexecSelfResponsible` in daemon |
| **Onboarding UI** (dialog + Settings-anchored drag bar, Rust/AppKit) | §10 | ✅ built + E2E (`TestSmokeBundleOnboardingSpawn`); visuals verified via `--demo-shot` | `cmd/jcode-computerd/onboarding/`, `surfaceOnboardingUI` in daemon |
| Helper icon drawn in code + committed .icns | §10 | ✅ | `onboarding/src/icon.rs`, `script/render_computerd_icon.sh`, `cmd/jcode-computerd/icons/` |
| Bundle in Tauri desktop packaging (`Contents/Resources/jcode-computerd.app`, bare computerd sidecars removed) | §6,§10 | ✅ `make desktop-sidecar` builds the bundle; dial resolves `../Resources` | `Makefile`, `tauri.macos.conf.json`, `helper_dial.go` |
| Bundle in CLI release assets (release.yml, install.sh, `make install`) | §6,§10 | ⬜ deferred — CLI installs still ship bare binaries (bare flow keeps working) | — |

**The honest gaps**, restated plainly: daemon-instance PID+token admission is
built, but a same-uid process can still launch a new authorized helper and mutual
audit-token/code-signature identity is not built; continuous same-app human
takeover detection is not built; lock-screen paths are not safely automatable in
CI. Historical Windows notes elsewhere in this document are research archive,
not a roadmap or a product fallback. The
macOS AX/action/capture path has now been driven against a real app with real
permissions, including recovery after a dead daemon and survival after capture.
