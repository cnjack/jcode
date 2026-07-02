// cdp-verify.mjs — drives real Chrome via the DevTools Protocol (Node 22 built-in WebSocket/fetch).
// Loads verify.html (which loads the real ./index.html in an iframe) and captures ALL page
// console output + JS exceptions, then reads the harness result. Proves no console errors.
import { spawn } from 'node:child_process';

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const PORT = 9333;
const URL = 'http://127.0.0.1:8099/tests/verify.html';
const PROFILE = '/tmp/cd-circuit-profile';

const chrome = spawn(CHROME, [
  '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
  '--remote-debugging-port=' + PORT, '--user-data-dir=' + PROFILE,
  '--disable-features=ChromeWhatsNewUI', // quiet
  'about:blank',
], { stdio: 'ignore' });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function getWsUrl() {
  for (let i = 0; i < 60; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/list`);
      const list = await res.json();
      const page = list.find((t) => t.type === 'page');
      if (page && page.webSocketDebuggerUrl) return page.webSocketDebuggerUrl;
    } catch {}
    await sleep(250);
  }
  throw new Error('devtools never came up');
}

const events = []; // console / exception log
let ws, id = 0;
const pending = new Map();
function send(method, params = {}) {
  const msgId = ++id;
  return new Promise((resolve, reject) => {
    pending.set(msgId, { resolve, reject });
    ws.send(JSON.stringify({ id: msgId, method, params }));
  });
}

function fmtRemote(obj) {
  try { return obj.value !== undefined ? JSON.stringify(obj.value) : (obj.description || obj.unserializableValue || obj.type); }
  catch { return String(obj); }
}

try {
  const wsUrl = await getWsUrl();
  await new Promise((resolve, reject) => {
    ws = new WebSocket(wsUrl);
    ws.addEventListener('open', resolve);
    ws.addEventListener('error', reject);
    setTimeout(() => reject(new Error('ws open timeout')), 8000);
  });

  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      const p = pending.get(msg.id); pending.delete(msg.id);
      msg.error ? p.reject(new Error(msg.error.message)) : p.resolve(msg.result);
      return;
    }
    if (msg.method === 'Runtime.consoleAPICalled') {
      const { type, args } = msg.params;
      events.push(`[console.${type}] ` + args.map(fmtRemote).join(' '));
    } else if (msg.method === 'Runtime.exceptionThrown') {
      const d = msg.params.exceptionDetails;
      events.push(`[exception] ` + (d.exception ? (d.exception.description || d.exception.value) : d.text) + (d.url ? ' @ ' + d.url : ''));
    } else if (msg.method === 'Log.entryAdded') {
      const e = msg.params.entry;
      events.push(`[log:${e.level}] ${e.text}`);
    }
  });

  await send('Runtime.enable');
  await send('Log.enable');
  await send('Page.enable');
  await send('Page.navigate', { url: URL });

  // Poll for harness completion (real time).
  let result = null;
  for (let i = 0; i < 80; i++) {
    await sleep(250);
    const r = await send('Runtime.evaluate', {
      expression: `(function(){ var el=document.getElementById('r'); return el && el.textContent ? el.textContent : (window.__done?'__done_no_text':''); })()`,
      returnByValue: true,
    });
    const val = r.result && r.result.value;
    if (val && typeof val === 'string' && val.length) { result = val; break; }
  }

  console.log('=== HARNESS RESULT ===');
  console.log(result || '(harness did not produce a result)');
  console.log('=== ALL PAGE CONSOLE / EXCEPTION OUTPUT ===');
  if (events.length === 0) console.log('(no console output and no exceptions)');
  else events.forEach((e) => console.log(e));
} catch (e) {
  console.error('CDP error:', e.message);
} finally {
  try { await send('Browser.close'); } catch {}
  await sleep(300);
  chrome.kill('SIGKILL');
}
