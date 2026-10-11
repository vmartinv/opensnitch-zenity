// usage: node cdp-shot.mjs <url> <out.png> <profile-dir>
// Loads url in headless Chromium, waits for GTK Broadway to paint, and saves
// a screenshot. Plain --screenshot fires before Broadway has drawn anything.
import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';

const [url, out, profile] = process.argv.slice(2);
const port = 9333;
const chrome = spawn('chromium', [
  '--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars',
  '--window-size=900,640', `--remote-debugging-port=${port}`,
  `--user-data-dir=${profile}`, 'about:blank',
], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));

let targets;
for (let i = 0; i < 50 && !targets; i++) {
  try { targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json(); }
  catch { await sleep(200); }
}
const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));
let id = 0;
const pending = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  pending.get(m.id)?.(m.result);
  pending.delete(m.id);
});
const send = (method, params = {}) => new Promise(r => {
  pending.set(++id, r);
  ws.send(JSON.stringify({ id, method, params }));
});

await send('Emulation.setDeviceMetricsOverride', { width: 900, height: 640, deviceScaleFactor: 2, mobile: false });
await send('Page.navigate', { url });
await sleep(4000);
const { data } = await send('Page.captureScreenshot', { format: 'png' });
writeFileSync(out, Buffer.from(data, 'base64'));
ws.close();
chrome.kill();
