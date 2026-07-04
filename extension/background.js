// jcode Browser Bridge — MV3 service worker.
//
// Connects a websocket to the local jcode server and relays Chrome DevTools
// Protocol commands to the user's tabs via chrome.debugger. The server drives
// everything; this worker is a thin, auth-gated forwarder. See
// internal/browser/bridge.go for the envelope format.

const DEFAULT_SERVER = "ws://127.0.0.1:8080/api/browser/ext/ws";
const NATIVE_HOST = "com.jcode.bridge";
const DEBUGGER_VERSION = "1.3";
const GROUP_TITLE = "jcode 🔎";

let ws = null;
let connected = false;
let reconnectDelay = 1000;
let reconnectTimer = null; // handle so Disconnect can cancel a queued retry
let connectTimer = null;   // handle for the connect-stall timeout
let attempts = 0; // consecutive failed connects; bounded so a wrong URL gives up
let desired = false; // user intent: should we be connected? Disconnect = false.
const MAX_ATTEMPTS = 6;
const CONNECT_TIMEOUT_MS = 8000;
const attached = new Set(); // tab ids we hold a debugger on
let lastError = ""; // surfaced to the popup so failures aren't silent

// ---- storage helpers ----
async function getConfig() {
  const { serverUrl, token } = await chrome.storage.local.get(["serverUrl", "token"]);
  return { serverUrl: serverUrl || DEFAULT_SERVER, token: token || "" };
}
async function setToken(token) {
  await chrome.storage.local.set({ token });
}

// stop is the single hard-off switch: it tears down the socket, cancels any
// queued reconnect, and (optionally) forgets credentials so nothing — not the
// onclose handler, not the keepalive alarm — can bring the connection back until
// the user pairs again. This is what makes Disconnect actually stop.
function stop(forget) {
  desired = false;
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  if (connectTimer) { clearTimeout(connectTimer); connectTimer = null; }
  if (ws) {
    ws.onclose = null; ws.onerror = null; ws.onmessage = null; ws.onopen = null;
    try { ws.close(); } catch {}
    ws = null;
  }
  connected = false;
  chrome.action.setBadgeText({ text: "" });
  if (forget) chrome.storage.local.remove("token");
}

// nativeConnect asks the jcode desktop/CLI app (via the native-messaging host)
// for the current server URL + a token, then dials it. This is the zero-input
// path: no port to know, no code to type, and it self-heals a changed dynamic
// port. Returns a promise that resolves to "" on success or an error string.
function nativeConnect() {
  return new Promise((resolve) => {
    let port;
    try {
      port = chrome.runtime.connectNative(NATIVE_HOST);
    } catch (e) {
      resolve("Native host unavailable: " + String(e && e.message ? e.message : e));
      return;
    }
    let settled = false;
    const done = (msg) => { if (!settled) { settled = true; try { port.disconnect(); } catch {} resolve(msg); } };

    port.onMessage.addListener(async (m) => {
      if (m && m.ws && m.token) {
        await chrome.storage.local.set({ serverUrl: m.ws, token: m.token });
        lastError = "";
        reconnectDelay = 1000;
        attempts = 0;
        triedNativeRediscover = false;
        desired = true;
        if (ws) { try { ws.onclose = null; ws.close(); } catch {} ws = null; }
        connect();
        done("");
      } else {
        done((m && m.error) || "jcode did not return an endpoint (is it running with browser use enabled?)");
      }
    });
    port.onDisconnect.addListener(() => {
      const e = chrome.runtime.lastError;
      done(e ? "Native host error: " + e.message + " — is jcode installed and running?" : "");
    });
    // Nudge the host in case it waits for a request.
    try { port.postMessage({ type: "get_endpoint" }); } catch {}
  });
}

// ---- connection ----
async function connect() {
  if (!desired) return; // Disconnect / gave-up state — never reconnect on its own.
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;
  const { serverUrl, token } = await getConfig();
  if (!token) {
    desired = false; // no token yet — wait for Auto-connect to fetch one.
    return;
  }
  let sock;
  try {
    sock = new WebSocket(serverUrl);
  } catch (e) {
    lastError = "Bad server URL: " + String(e && e.message ? e.message : e);
    scheduleReconnect();
    return;
  }
  ws = sock;
  // Every handler below guards on `ws === sock`. Handlers are bound to this
  // specific socket but mutate module-level state (`connected`, `lastError`);
  // once a newer connect() has replaced the global `ws`, this socket is
  // superseded and a late-firing event from it (especially onclose) must NOT
  // clobber the live connection's state — that desync is what stranded the
  // popup on "Reconnecting…" while the socket was actually up.

  // Connect-stall watchdog. When the extension lacks host access to the target
  // (e.g. 127.0.0.1 site access is off in edge://extensions), the WebSocket
  // neither opens nor errors — it just hangs. Fail loudly after a timeout with a
  // message that points at the real fix instead of spinning on "Connecting…".
  if (connectTimer) clearTimeout(connectTimer);
  connectTimer = setTimeout(() => {
    connectTimer = null;
    if (ws === sock && !connected && sock.readyState !== WebSocket.OPEN) {
      lastError =
        "Connection stalled (no response from " + serverUrl + "). " +
        "Most likely the extension lacks access to this host — open the extensions page › this extension › " +
        "Site access and allow 127.0.0.1 / localhost (set to 'On all sites'). Then reload the extension and Auto-connect again.";
      try { sock.close(); } catch {}
    }
  }, CONNECT_TIMEOUT_MS);

  sock.onopen = () => {
    if (ws !== sock) return;
    lastError = "";
    sock.send(JSON.stringify({ type: "hello", token }));
  };

  sock.onmessage = async (ev) => {
    if (ws !== sock) return;
    let msg;
    try { msg = JSON.parse(ev.data); } catch { return; }
    if (msg.type === "welcome") {
      if (connectTimer) { clearTimeout(connectTimer); connectTimer = null; }
      connected = true;
      reconnectDelay = 1000;
      attempts = 0;
      triedNativeRediscover = false;
      if (msg.token) await setToken(msg.token);
      chrome.action.setBadgeText({ text: "on" });
      chrome.action.setBadgeBackgroundColor({ color: "#1f9d55" });
      return;
    }
    if (msg.type === "error") {
      if (connectTimer) { clearTimeout(connectTimer); connectTimer = null; }
      lastError = msg.message || "server rejected the connection";
      chrome.action.setBadgeText({ text: "!" });
      chrome.action.setBadgeBackgroundColor({ color: "#c73a2f" });
      // Stale token: forget it and stop; Auto-connect will fetch a fresh one.
      stop(true);
      return;
    }
    // Server keepalive: the inbound frame itself is what keeps this MV3 worker
    // awake (Chrome resets the idle timer on any ws message); the pong lets the
    // server confirm we're alive. Receiving a ping also proves we're connected,
    // so self-heal the flag in case a stale event left it out of sync.
    if (msg.type === "ping") {
      if (!connected) { connected = true; chrome.action.setBadgeText({ text: "on" }); }
      send({ type: "pong" });
      return;
    }
    if (msg.type === "pong") { return; }
    await handleEnvelope(msg);
  };

  sock.onclose = () => {
    if (ws !== sock) return; // superseded socket — leave the live one alone
    if (connectTimer) { clearTimeout(connectTimer); connectTimer = null; }
    const wasConnected = connected;
    connected = false;
    chrome.action.setBadgeText({ text: "" });
    // Dropping a live connection is routine (MV3 worker nap, jcode restart) —
    // don't cry wolf. Reconnect quietly and let the badge/pill show "Reconnecting…".
    // Only surface the hard "can't reach" error once a few attempts in a row
    // have failed, i.e. the server really is gone or on a stale port.
    if (wasConnected) {
      lastError = "";
      reconnectDelay = 1000;
      attempts = 0;
    } else if (!lastError && attempts >= 3) {
      lastError = "Could not reach the jcode server. Check that jcode is running and the URL/port is right.";
    }
    scheduleReconnect();
  };
  sock.onerror = () => {
    if (ws !== sock) return;
    lastError = "WebSocket error connecting to " + serverUrl + " — is jcode running there, and does the extension have site access to it?";
    try { sock.close(); } catch {}
  };
}

let triedNativeRediscover = false;

function scheduleReconnect() {
  if (!desired) return;
  attempts += 1;
  if (attempts >= MAX_ATTEMPTS) {
    // The saved URL is dead — most often the app restarted on a new dynamic
    // port. Try the native host once to rediscover the current endpoint before
    // giving up (self-heals without any user action).
    if (!triedNativeRediscover) {
      triedNativeRediscover = true;
      nativeConnect().then((err) => {
        if (err) {
          lastError = (lastError || "Connection failed") + " — gave up. Reconnect from jcode settings.";
          stop(false);
        }
      });
      return;
    }
    lastError = (lastError || "Connection failed") + " — gave up after several tries. Reconnect from jcode settings.";
    stop(false);
    return;
  }
  reconnectDelay = Math.min(reconnectDelay * 2, 30000);
  if (reconnectTimer) clearTimeout(reconnectTimer);
  reconnectTimer = setTimeout(() => { reconnectTimer = null; connect(); }, reconnectDelay);
}

function send(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj));
}

// ---- envelope dispatch ----
async function handleEnvelope(msg) {
  const id = msg.id;
  try {
    switch (msg.type) {
      case "tab.new": {
        const tab = await chrome.tabs.create({ url: msg.url || "about:blank", active: false });
        await attachTab(tab.id);
        await groupTab(tab.id);
        send({ type: "tab.result", id, tabId: String(tab.id) });
        break;
      }
      case "tab.attach": {
        const tabId = parseInt(msg.tabId, 10);
        await attachTab(tabId);
        await groupTab(tabId);
        send({ type: "tab.result", id, tabId: String(tabId) });
        break;
      }
      case "tab.close": {
        const tabId = parseInt(msg.tabId, 10);
        await detachTab(tabId);
        try { await chrome.tabs.remove(tabId); } catch {}
        send({ type: "tab.result", id, tabId: msg.tabId });
        break;
      }
      case "tab.detach": {
        const tabId = parseInt(msg.tabId, 10);
        await detachTab(tabId);
        send({ type: "tab.result", id, tabId: msg.tabId });
        break;
      }
      case "tabs.list": {
        const tabs = await chrome.tabs.query({});
        const list = tabs
          .filter((t) => t.url && /^https?:/.test(t.url))
          .map((t) => ({ id: String(t.id), title: t.title || "", url: t.url, user_tab: !attached.has(t.id) }));
        send({ type: "tabs.result", id, tabs: list });
        break;
      }
      case "cdp.send": {
        const tabId = parseInt(msg.tabId, 10);
        // msg.params is already a parsed JS object (Go sends it as raw JSON in
        // the envelope, so JSON.parse of the whole frame yields an object).
        const result = await sendCDP(tabId, msg.method, msg.params);
        // Send result as a real JSON object; Go captures it as json.RawMessage.
        send({ type: "cdp.result", id, result: result ?? {} });
        break;
      }
      default:
        send({ type: "cdp.error", id, error: "unknown envelope type " + msg.type });
    }
  } catch (e) {
    send({ type: "cdp.error", id, error: String(e && e.message ? e.message : e) });
  }
}

// ---- chrome.debugger plumbing ----
function attachTab(tabId) {
  return new Promise((resolve, reject) => {
    if (attached.has(tabId)) return resolve();
    chrome.debugger.attach({ tabId }, DEBUGGER_VERSION, () => {
      if (chrome.runtime.lastError) return reject(new Error(chrome.runtime.lastError.message));
      attached.add(tabId);
      resolve();
    });
  });
}

function detachTab(tabId) {
  return new Promise((resolve) => {
    if (!attached.has(tabId)) return resolve();
    chrome.debugger.detach({ tabId }, () => {
      attached.delete(tabId);
      resolve();
    });
  });
}

function sendCDP(tabId, method, params) {
  return new Promise((resolve, reject) => {
    chrome.debugger.sendCommand({ tabId }, method, params || {}, (result) => {
      if (chrome.runtime.lastError) return reject(new Error(chrome.runtime.lastError.message));
      resolve(result);
    });
  });
}

async function groupTab(tabId) {
  try {
    const groupId = await chrome.tabs.group({ tabIds: [tabId] });
    await chrome.tabGroups.update(groupId, { title: GROUP_TITLE, color: "orange" });
  } catch {}
}

// Forward CDP events for attached tabs.
chrome.debugger.onEvent.addListener((source, method, params) => {
  if (source.tabId == null || !attached.has(source.tabId)) return;
  send({ type: "cdp.event", tabId: String(source.tabId), method, params: params ?? {} });
});

// User (or Chrome) detached the debugger — the user took control back.
chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId != null) {
    attached.delete(source.tabId);
    send({ type: "cdp.event", tabId: String(source.tabId), method: "Inspector.detached", params: {} });
  }
});

// ---- popup ↔ worker messaging ----
chrome.runtime.onMessage.addListener((req, _sender, sendResponse) => {
  (async () => {
    switch (req.type) {
      case "native_connect": {
        // Zero-input connect via the jcode native host.
        const err = await nativeConnect();
        sendResponse({ ok: !err, error: err });
        break;
      }
      case "status": {
        sendResponse({
          connected,
          controlled: [...attached].map(String),
          lastError,
          desired,
        });
        break;
      }
      case "disconnect":
        // Hard stop: detach tabs, tear down the socket, cancel retries, forget
        // the token. Nothing reconnects until the user runs Auto-connect again.
        for (const tabId of [...attached]) await detachTab(tabId);
        stop(true);
        lastError = "";
        sendResponse({ ok: true });
        break;
      default:
        sendResponse({ ok: false });
    }
  })();
  return true; // async response
});

// resume re-arms the connection from a saved token (worker wake / browser
// start). It never fires from a wrong pairing attempt — only a stored token, so
// after Disconnect (token forgotten) nothing comes back on its own.
async function resume() {
  const { token } = await getConfig();
  if (token) {
    desired = true;
    attempts = 0;
    connect();
  }
}

// ---- keepalive / lifecycle (MV3 worker may sleep) ----
// Guard the alarms wiring: if the "alarms" permission is ever missing,
// chrome.alarms is undefined — do NOT let that throw at top level and take the
// whole service worker down (that would break pairing entirely). Pairing itself
// works without alarms because an open popup keeps the worker alive.
try {
  if (chrome.alarms) {
    chrome.alarms.create("keepalive", { periodInMinutes: 0.5 });
    chrome.alarms.onAlarm.addListener((a) => {
      if (a.name !== "keepalive") return;
      if (!desired) return; // respect a hard stop; don't silently reconnect.
      if (connected) send({ type: "ping" });
      else connect();
    });
  } else {
    console.warn("jcode bridge: chrome.alarms unavailable (missing permission); keepalive disabled");
  }
} catch (e) {
  console.warn("jcode bridge: alarm setup failed:", e);
}
chrome.runtime.onStartup.addListener(resume);
chrome.runtime.onInstalled.addListener(resume);
resume();
