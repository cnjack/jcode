// Popup UI for the jcode Browser Bridge. Talks to the service worker over
// chrome.runtime messaging. Single connect path: Auto-connect (native host).

const $ = (id) => document.getElementById(id);

function send(msg) {
  return new Promise((resolve) => {
    try {
      chrome.runtime.sendMessage(msg, (resp) => {
        // Swallow "receiving end does not exist" (worker asleep) — resolve null.
        void chrome.runtime.lastError;
        resolve(resp);
      });
    } catch {
      resolve(null);
    }
  });
}

function showMsg(text, kind) {
  const el = $("msg");
  if (!text) {
    el.style.display = "none";
    return;
  }
  el.textContent = text;
  el.className = "msg " + (kind || "err");
  el.style.display = "";
}

async function refresh() {
  const st = await send({ type: "status" });
  if (!st) return;
  const pill = $("status");
  if (st.connected) {
    pill.className = "pill on";
    pill.innerHTML = '<span class="dot on"></span>Connected';
    $("autoConnect").textContent = "Reconnect";
    showMsg("", null);
  } else if (st.desired) {
    pill.className = "pill off";
    pill.innerHTML = '<span class="dot off"></span>Reconnecting…';
    $("autoConnect").textContent = "Auto-connect to jcode";
    // Only go red on a real failure; a routine reconnect stays neutral.
    if (st.lastError) {
      showMsg(st.lastError + " Click Disconnect to stop trying.", "err");
    } else {
      showMsg("Reconnecting to jcode…", "info");
    }
  } else {
    pill.className = "pill off";
    pill.innerHTML = '<span class="dot off"></span>Offline';
    $("autoConnect").textContent = "Auto-connect to jcode";
    if (st.lastError) showMsg(st.lastError, "err");
  }
  const tabs = $("tabs");
  if (st.controlled && st.controlled.length) {
    tabs.innerHTML = "";
    for (const id of st.controlled) {
      const row = document.createElement("div");
      row.className = "tabrow";
      row.innerHTML = `<span class="badge">jcode</span><span>tab ${id}</span>`;
      tabs.appendChild(row);
    }
  } else {
    tabs.innerHTML = '<div class="muted">None — jcode is not driving any tab.</div>';
  }
}

$("autoConnect").addEventListener("click", async () => {
  showMsg("Finding jcode…", "ok");
  $("autoConnect").disabled = true;
  const resp = await send({ type: "native_connect" });
  $("autoConnect").disabled = false;
  if (resp && resp.ok) {
    showMsg("Connecting…", "ok");
    setTimeout(refresh, 500);
  } else {
    showMsg((resp && resp.error) || "Could not reach the jcode app. Is it running with browser use enabled?", "err");
  }
});

$("disconnect").addEventListener("click", async () => {
  await send({ type: "disconnect" });
  showMsg("Stopped. Not connected.", "ok");
  refresh();
});

refresh();
setInterval(refresh, 2000);
