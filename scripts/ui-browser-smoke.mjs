// SPDX-License-Identifier: Apache-2.0
// Opt-in real Chrome CDP / Firefox BiDi smoke. Private test-owned profile, no downloads.
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";
import assert from "node:assert/strict";

const url = process.env.GRILLO_UI_TEST_URL;
const live = process.env.GRILLO_UI_TEST_LIVE === "1";
assert(url, "GRILLO_UI_TEST_URL is required (never print its token)");
const chrome = process.env.GRILLO_UI_BROWSER === 'chrome';
const browserName = chrome ? 'Chrome' : 'Firefox';
const profile = await mkdtemp(join(tmpdir(), chrome ? 'grillo-chrome-' : 'grillo-firefox-'));
await writeFile(join(profile,"user.js"), `user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("datareporting.policy.dataSubmissionEnabled", false);
user_pref("toolkit.telemetry.enabled", false);
user_pref("app.update.auto", false);
user_pref("browser.startup.homepage", "about:blank");
user_pref("browser.newtabpage.enabled", false);
user_pref("network.captive-portal-service.enabled", false);
user_pref("network.connectivity-service.enabled", false);
`, {mode:0o600});
const listener = net.createServer();
await new Promise(resolve => listener.listen(0,"127.0.0.1",resolve));
const port = listener.address().port;
await new Promise(resolve => listener.close(resolve));
const browser = spawn(chrome ? 'google-chrome' : 'firefox', chrome ? ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--disable-component-update', '--disable-sync', '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=' + port, '--user-data-dir=' + profile, 'about:blank'] : ["--headless","--no-remote","--profile",profile,"--remote-debugging-port",String(port),"about:blank"], {stdio:["ignore","pipe","pipe"]});
let exited = false, diagnostics = "", ws;
const exitPromise = new Promise(resolve => {
  browser.once("exit",(code,signal)=>{exited=true;resolve({code,signal});});
  browser.once("error",e=>{diagnostics=e.message;exited=true;resolve({error:e.message});});
});
for (const stream of [browser.stdout,browser.stderr]) stream.on("data",chunk=>{diagnostics=(diagnostics+chunk).slice(-8192);});
const pending = new Map(); let nextID = 0;
try {
  for (let i=0;i<100;i++) {
    if (exited) throw new Error(browserName + ' exited before remote handshake: ' + diagnostics);
    try {
      let endpoint = `ws://127.0.0.1:${port}/session`;
      if (chrome) {
        const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`, { signal: AbortSignal.timeout(500) })).json();
        endpoint = pages.find(page => page.type === 'page')?.webSocketDebuggerUrl;
        assert(endpoint, 'Chrome test page not available');
      }
      ws = new WebSocket(endpoint);
      await new Promise((resolve,reject)=>{
        const timer=setTimeout(()=>reject(new Error("BiDi connection timeout")),500);
        ws.addEventListener("open",()=>{clearTimeout(timer);resolve();},{once:true});
        ws.addEventListener("error",e=>{clearTimeout(timer);reject(e);},{once:true});
      });
      break;
    } catch { ws?.close(); ws=null; await new Promise(r=>setTimeout(r,100)); }
  }
  assert(ws, browserName + ' remote protocol unavailable: ' + diagnostics);
  ws.addEventListener("message",event=>{
    const message=JSON.parse(event.data), waiter=pending.get(message.id);
    if (!waiter) return;
    pending.delete(message.id);
    if (message.type === 'error' || message.error) waiter.reject(new Error(JSON.stringify(message.error) + ': ' + (message.message || ''))); else waiter.resolve(message.result);
  });
  function rawCommand(method,params) {
    const id=++nextID;
    return new Promise((resolve,reject)=>{
      const timer=setTimeout(()=>{pending.delete(id);reject(new Error("BiDi timeout: "+method));},10000);
      pending.set(id,{resolve:r=>{clearTimeout(timer);resolve(r);},reject:e=>{clearTimeout(timer);reject(e);}});
      ws.send(JSON.stringify({id,method,params}));
    });
  }
  async function command(method, params) {
    if (!chrome) return rawCommand(method, params);
    if (method === 'session.new') { await rawCommand('Page.enable', {}); return {}; }
    if (method === 'session.end') return {};
    if (method === 'test.key') {
      await rawCommand('Input.dispatchKeyEvent', { type: 'keyDown', ...params });
      return rawCommand('Input.dispatchKeyEvent', { type: 'keyUp', ...params });
    }
    if (method === 'test.setColorScheme') return rawCommand('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: params.value }] });
    if (method === 'browsingContext.create') return { context: 'test-page' };
    if (method === 'script.addPreloadScript') return rawCommand('Page.addScriptToEvaluateOnNewDocument', { source: '(' + params.functionDeclaration + ')()' });
    if (method === 'browsingContext.setViewport') return rawCommand('Emulation.setDeviceMetricsOverride', { ...params.viewport, deviceScaleFactor: 1, mobile: false });
    if (method === 'browsingContext.navigate') {
      await rawCommand('Page.navigate', { url: params.url });
      for (let i = 0; i < 100; i++) {
        const result = await rawCommand('Runtime.evaluate', { expression: 'document.readyState', returnByValue: true });
        if (result.result.value === 'complete') return {};
        await new Promise(resolve => setTimeout(resolve, 50));
      }
      throw new Error('Chrome navigation timeout');
    }
    if (method === 'script.evaluate') {
      const result = await rawCommand('Runtime.evaluate', { expression: params.expression, awaitPromise: true, returnByValue: true });
      return result.exceptionDetails ? { type: 'exception', exceptionDetails: result.exceptionDetails } : { type: 'success', result: result.result };
    }
    if (method === 'input.performActions') {
      for (const action of params.actions.flatMap(group => group.actions)) {
        assert.equal(action.value, '\uE007');
        await rawCommand('Input.dispatchKeyEvent', { type: action.type, key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, ...(action.type === 'keyDown' ? { text: '\r' } : {}) });
      }
      return {};
    }
    if (method === 'browsingContext.captureScreenshot') return rawCommand('Page.captureScreenshot', { format: 'png' });
    if (method === 'browsingContext.close') return rawCommand('Page.close', {});
    throw new Error('Unsupported browser test command: ' + method);
  }
  await command("session.new",{capabilities:{}});
  await command("script.addPreloadScript", {functionDeclaration: `() => {
    window.__cspErrors = []; window.__runtimeErrors = [];
    addEventListener('securitypolicyviolation', e => window.__cspErrors.push(e.violatedDirective));
    addEventListener('error', e => window.__runtimeErrors.push(e.message));
  }`});
  const {context}=await command("browsingContext.create",{type:"tab"});
  await command("browsingContext.setViewport", {context, viewport: {width: 1440, height: 1000}});
  if (chrome) await command('test.setColorScheme', { value: 'dark' });
  await command("browsingContext.navigate",{context,url,wait:"complete"});
  async function evaluate(expression) {
    const r=await command("script.evaluate",{expression:`(async () => JSON.stringify((await (${expression})) ?? null))()`,target:{context},awaitPromise:true});
    if (r.type!=="success") throw new Error("Browser JavaScript exception: "+JSON.stringify(r.exceptionDetails));
    return JSON.parse(r.result.value);
  }
  async function wait(expression) {
    for (let i=0;i<100;i++) { if(await evaluate(expression)) return; await new Promise(r=>setTimeout(r,100)); }
    throw new Error("Browser condition timed out: "+expression+"; status: "+await evaluate(`document.getElementById("exec-status")?.textContent || [...document.querySelectorAll('#terminal-output .xterm-rows > div')].map(row => row.textContent).join('\\n').slice(-2000) || document.body.textContent.slice(0, 700)`));
  }
  async function navigate(section) {
    await evaluate(`document.querySelector('[data-testid="nav-${section}"]').click()`);
    await wait(`document.querySelector('[data-testid="nav-${section}"]').getAttribute('aria-current') === 'page'`);
  }
  async function setValue(id, value) {
    await evaluate(`(() => {
      const input = document.getElementById(${JSON.stringify(id)});
      Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), 'value').set.call(input, ${JSON.stringify(value)});
      input.dispatchEvent(new Event('input', {bubbles:true})); input.dispatchEvent(new Event('change', {bubbles:true}));
    })()`);
  }
  await wait(`document.getElementById("overview")?.textContent.includes("observed Pods")`);
  assert(await evaluate(`!document.getElementById('overview').textContent.includes('MicroVM')`), 'overview is VM-centric');
  assert(await evaluate(`location.hash === ""`),"bootstrap fragment not removed");
  assert(await evaluate(`document.querySelector('img.gr-brand-mark').complete && document.querySelector('img.gr-brand-mark').naturalWidth > 0`), 'supplied brand icon missing');
  assert(await evaluate(`document.querySelector('link[rel="icon"][type="image/svg+xml"]').href.endsWith('/assets/generated/icons/favicon.svg') && !!document.querySelector('link[rel="apple-touch-icon"]')`), 'favicon/touch metadata missing');
  const iconMetadata = await evaluate(`(async () => { const response = await fetch(document.querySelector('link[rel="manifest"]').href); const manifest = await response.json(); const icon = await fetch('/favicon.ico'); return { status: response.status, icons: manifest.icons.length, start: manifest.start_url, favicon: icon.status }; })()`);
  assert.deepEqual(iconMetadata, { status: 200, icons: 2, start: '/', favicon: 200 }, 'invalid embedded icon manifest');
  if (chrome) await wait(`document.documentElement.dataset.theme === 'dark' && document.getElementById('theme-preference').value === 'system'`);
  await setValue('theme-preference', 'light');
  await wait(`document.documentElement.dataset.theme === 'light'`);
  await setValue('theme-preference', 'dark');
  await wait(`document.documentElement.classList.contains('pf-v6-theme-dark')`);
  assert(await evaluate(`localStorage.getItem('grillo.theme') === 'dark'`), 'manual theme not persisted');
  assert(await evaluate(`getComputedStyle(document.querySelector('.pf-v6-c-card')).backgroundColor === 'rgb(34, 43, 50)'`), 'custom cards did not adopt dark theme');
  assert.deepEqual(await evaluate(`window.__cspErrors`), [], 'theme/icon integration violates CSP');
  if (process.env.GRILLO_UI_SCREENSHOT) {
    const shot = await command('browsingContext.captureScreenshot', { context, format: { type: 'image/png' } });
    await writeFile(process.env.GRILLO_UI_SCREENSHOT.replace('.png', '-dark.png'), Buffer.from(shot.data, 'base64'));
  }
  await command('browsingContext.navigate', { context, url: url.split('#')[0], wait: 'complete' });
  await wait(`document.getElementById('overview') && document.getElementById('theme-preference')?.value === 'dark' && document.documentElement.dataset.theme === 'dark'`);
  if (chrome) {
    await command('test.setColorScheme', { value: 'light' });
    assert(await evaluate(`document.documentElement.dataset.theme === 'dark'`), 'browser preference overrode explicit dark');
    await setValue('theme-preference', 'system');
    await wait(`document.documentElement.dataset.theme === 'light'`);
    await command('test.setColorScheme', { value: 'dark' });
    await wait(`document.documentElement.dataset.theme === 'dark'`);
    await command('test.setColorScheme', { value: 'light' });
    await wait(`document.documentElement.dataset.theme === 'light'`);
  } else {
    await setValue('theme-preference', 'light');
    await wait(`document.documentElement.dataset.theme === 'light'`);
  }
  assert(await evaluate(`!!document.querySelector('.pf-v6-c-page[data-console="patternfly-react"]')`),"PatternFly React not mounted");
  await navigate('metrics');
  assert(await evaluate(`document.getElementById("metrics").textContent.length > 0`),"missing metrics");
  if (live) {
    assert(await evaluate(`document.getElementById("metrics").textContent.includes("/proc")`),"no real VMM sample");
    assert(await evaluate(`document.getElementById("metrics").textContent.includes("guest /proc/meminfo") && document.getElementById("metrics").textContent.includes("guest cgroup v2") && document.getElementById("metrics").textContent.includes("µs")`),"no real guest/container usage samples");
  }
  for (const [section, ids] of [['workloads',['workloads','sandboxes']],['controllers',['controllers']],['networking',['services']],['routes',['routes']],['storage',['volumes']],['configuration',['configs']],['secrets',['secrets']],['diagnostics',['diagnostics']]]) {
    await navigate(section);
    for (const id of ids) assert(await evaluate(`document.getElementById(${JSON.stringify(id)}).textContent.length > 0`),"empty pane: "+id);
  }
  await navigate('topology');
  assert(await evaluate(`document.getElementById("topology").querySelectorAll("rect").length >= 1`),"missing topology");
  await evaluate(`document.querySelector('#topology [data-kind="service"]').focus()`);
  await command('input.performActions', {context, actions:[{type:'key', id:'topology-keyboard', actions:[{type:'keyDown', value:'\uE007'}, {type:'keyUp', value:'\uE007'}]}]});
  await wait(`document.getElementById('topology-detail')?.textContent.includes('Instances / Pods')`);
  assert(await evaluate(`document.querySelectorAll('#topology path, #topology [data-kind="workload"], #topology [data-kind="route"]').length === 0`), 'overview includes connections or non-Service resources');
  assert(await evaluate(`document.querySelector('#topology [data-kind="service"]').getAttribute('aria-pressed') === 'true'`), 'keyboard selection not exposed');
  assert(await evaluate(`getComputedStyle(document.querySelector('.gr-masthead')).backgroundColor === 'rgb(16, 43, 53)'`), 'Grillo masthead palette missing');
  assert(await evaluate(`getComputedStyle(document.getElementById('refresh')).color === 'rgb(8, 114, 108)'`), 'Grillo action palette missing');
  assert(await evaluate(`document.getElementById('topology-detail').textContent.includes('Declared TCP ports')`), 'missing Service information');
  if (process.env.GRILLO_UI_SCREENSHOT) {
    const shot = await command('browsingContext.captureScreenshot', {context, format:{type:'image/png'}});
    await writeFile(process.env.GRILLO_UI_SCREENSHOT.replace('.png', '-topology.png'), Buffer.from(shot.data, 'base64'));
  }
  await evaluate(`document.querySelector('[aria-label="Close resource detail"]').click()`);
  await wait(`!document.getElementById('topology-detail')`);
  assert(await evaluate(`document.activeElement?.getAttribute('data-kind') === 'service'`), 'closing detail loses keyboard focus');
  await setValue('theme-preference', 'dark');
  await wait(`document.documentElement.dataset.theme === 'dark'`);
  assert(await evaluate(`['rgb(34, 43, 50)', 'rgb(32, 75, 69)', 'rgb(38, 61, 64)'].includes(getComputedStyle(document.querySelector('#topology rect')).fill) && getComputedStyle(document.querySelector('.gr-topology-canvas')).backgroundColor === 'rgb(23, 31, 37)' && getComputedStyle(document.querySelector('.gr-graph-name')).fill === 'rgb(230, 239, 242)'`), 'topology did not adopt dark theme, including focus/hover states');
  await setValue('theme-preference', 'light');
  await wait(`document.documentElement.dataset.theme === 'light'`);
  await setValue('topology-search', 'no-topology-match-unique');
  await wait(`document.querySelectorAll('#topology [role="button"]').length === 0`);
  await setValue('topology-search', '');
  await wait(`document.querySelectorAll('#topology [role="button"]').length >= 1`);
  await evaluate(`document.querySelector('[aria-label="Zoom in"]').click()`);
  assert(await evaluate(`!!document.querySelector('.gr-zoom-in')`), 'zoom control failed');
  await evaluate(`document.querySelector('[aria-label="Fit topology"]').click()`);
  assert(await evaluate(`!document.body.textContent.includes("synthetic-hidden-value") && !document.body.textContent.includes("synthetic-cli-private-token")`),"secret/config value in DOM");
  assert(await evaluate(`!document.cookie.includes("grillo_session")`),"session is not HttpOnly");
  const result=await evaluate(`(async()=>{const r=await fetch("/v1/applications/" + encodeURIComponent(document.getElementById("apps").value) + "/view"); return {status:r.status,body:await r.text()};})()`);
  assert.equal(result.status,200);assert(!result.body.includes("synthetic-hidden-value") && !result.body.includes("synthetic-cli-private-token"),"private value in public response");
  await navigate('workloads');
  await setValue('pod-search', 'no-pod-match-unique');
  await wait(`document.querySelectorAll('#sandboxes tbody tr').length === 0`);
  await setValue('pod-search', '');
  await wait(`document.querySelectorAll('#sandboxes tbody tr').length > 0`);
  const podID = await evaluate(`document.querySelector('#sandboxes tbody tr td button').textContent`);
  await evaluate(`Array.from(document.querySelectorAll('#sandboxes tbody tr:first-child button')).find(b => b.textContent === 'Logs').click()`);
  await wait(`document.getElementById('log-resource')?.value === ${JSON.stringify(podID)}`);
  assert(await evaluate(`document.getElementById('log-resource').disabled`), 'Pod logs can silently become global logs');
  await evaluate(`Array.from(document.querySelectorAll('#pod-detail [role="tab"]')).find(b => b.textContent === 'Details').click()`);
  await wait(`!!document.getElementById('pod-details')`);
  assert(await evaluate(`document.querySelector('#pod-details details').open === false`), 'runtime details are not secondary');
  await evaluate(`Array.from(document.querySelectorAll('#pod-detail [role="tab"]')).find(b => b.textContent === 'Events').click()`);
  await wait(`!!document.getElementById('pod-events')`);
  assert(await evaluate(`!document.getElementById('events').textContent.includes('application/demo')`), 'global events shown as Pod events');
  await evaluate(`Array.from(document.querySelectorAll('#pod-detail [role="tab"]')).find(b => b.textContent === 'Metrics').click()`);
  await wait(`!!document.getElementById('pod-metrics')`);
  assert(await evaluate(`document.getElementById('pod-metrics').textContent.includes(${JSON.stringify(podID)})`), 'missing scoped Pod metrics');
  await evaluate(`Array.from(document.querySelectorAll('#pod-detail [role="tab"]')).find(b => b.textContent === 'Terminal').click()`);
  await wait(`!!document.getElementById('terminal-connect')`);
  await evaluate(`document.getElementById('terminal-connect').click()`);
  await wait(`document.getElementById('terminal-status').textContent === 'Connected'`);
  const terminalTarget = podID + '/' + await evaluate(`document.getElementById('tty-container').value`);
  async function terminalPaste(text) {
    await evaluate(`document.getElementById('terminal-input').dispatchEvent(new ClipboardEvent('paste', { bubbles: true, clipboardData: (() => { const data = new DataTransfer(); data.setData('text', ${JSON.stringify(text)}); return data; })() }))`);
  }
  const terminalRows = `([...document.querySelectorAll('#terminal-output .xterm-rows > div')].map(row => row.textContent).join('\\n'))`;
  assert(await evaluate(`document.activeElement.id === 'terminal-input' && document.getElementById('terminal-input').classList.contains('xterm-helper-textarea')`), 'input is not integrated in xterm');
  const terminalText = live ? "echo $$ > /tmp/grillo-browser-terminal.pid; printf 'browser-terminal-marker\\n'; test -t 0 && echo real-tty-marker; printf 'term-marker-%s\\n' \"$TERM\"; stty size\n" : 'browser-terminal-marker\n';
  await terminalPaste(terminalText);
  await wait(`${terminalRows}.includes('browser-terminal-marker')`);
  if (live) {
    await wait(`${terminalRows}.includes('\\nreal-tty-marker') && ${terminalRows}.includes('\\nterm-marker-xterm-256color')`);
    await wait(`/\\n(\\d+) (\\d+)/.test(${terminalRows})`);
    const firstCols = await evaluate(`Number([...${terminalRows}.matchAll(/\\n(\\d+) (\\d+)/g)].at(-1)[2])`);
    await command('browsingContext.setViewport', { context, viewport: { width: 900, height: 1000 } });
    await new Promise(resolve => setTimeout(resolve, 200));
    await terminalPaste('stty size\n');
    await wait(`(() => { const cols = Number([...${terminalRows}.matchAll(/\\n(\\d+) (\\d+)/g)].at(-1)?.[2]); return Number.isFinite(cols) && cols !== ${firstCols}; })()`);
    const firstRows = await evaluate(`Number([...${terminalRows}.matchAll(/\\n(\\d+) (\\d+)/g)].at(-1)[1])`);
    await evaluate(`document.getElementById('terminal-output').style.height = '320px'`);
    await new Promise(resolve => setTimeout(resolve, 200)); await terminalPaste('stty size\n');
    await wait(`Number([...${terminalRows}.matchAll(/\\n(\\d+) (\\d+)/g)].at(-1)?.[1]) !== ${firstRows}`);
    await command('browsingContext.setViewport', { context, viewport: { width: 1440, height: 1000 } });
    await evaluate(`document.getElementById('terminal-output').style.height = '416px'`);
  }
  const enterScreen = '\x1b[?1049h\x1b[2J\x1b[H\x1b[31mfull-screen-marker\x1b[0m\x1b[2;1Hwide界\x1b[?25l';
  await terminalPaste(live ? "printf '\\033[?1049h\\033[2J\\033[H\\033[31mfull-screen-marker\\033[0m\\033[2;1Hwide界\\033[?25l'; read -r reply; printf '\\033[?25h\\033[?1049l'; echo fullscreen-returned\n" : enterScreen);
  await wait(`${terminalRows}.startsWith('full-screen-marker\\nwide界')`);
  assert(await evaluate(`!!document.querySelector('#terminal-output .xterm-fg-1') && !${terminalRows}.includes('browser-terminal-marker')`), 'ANSI color or alternate buffer missing');
  await terminalPaste(live ? 'leave-full-screen\n' : '\x1b[?25h\x1b[?1049l');
  await wait(`${terminalRows}.includes('browser-terminal-marker') && !${terminalRows}.startsWith('full-screen-marker')`);
  if (live) {
    await terminalPaste('/bin/busybox vi /tmp/grillo-browser-editor.txt\n');
    await wait(`${terminalRows}.includes('grillo-browser-editor.txt')`);
    await terminalPaste('ieditor-saved-marker');
    await wait(`${terminalRows}.includes('editor-saved-marker')`);
    if (chrome) await command('test.key', { key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
    else await terminalPaste('\x1b');
    await new Promise(resolve => setTimeout(resolve, 200));
    await terminalPaste(':wq\r');
    await new Promise(resolve => setTimeout(resolve, 200));
    await terminalPaste('cat /tmp/grillo-browser-editor.txt; echo editor-result-marker\n');
    await wait(`${terminalRows}.includes('\\neditor-saved-marker') && ${terminalRows}.includes('\\neditor-result-marker')`);
    await terminalPaste("sh -c 'echo $$ > /tmp/grillo-browser-terminal-child.pid; echo child-running; exec sleep 300'\n");
    await wait(`${terminalRows}.includes('\\nchild-running')`);
    if (chrome) await command('test.key', { key: 'c', code: 'KeyC', windowsVirtualKeyCode: 67, modifiers: 2 });
    else await evaluate(`document.getElementById('terminal-input').dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'c', code: 'KeyC', keyCode: 67, which: 67, ctrlKey: true }))`);
    await terminalPaste("printf 'interrupt-resumed\\n'\n");
    await wait(`${terminalRows}.includes('\\ninterrupt-resumed')`);
  } else {
    await terminalPaste('\x1b]2;untrusted-title\x07\x1b]52;c;Y2xpcGJvYXJk\x07\x1b]8;;https://example.invalid\x07link\x1b]8;;\x07');
    await wait(`${terminalRows}.includes('link')`);
    assert(await evaluate(`document.title === 'Grillo console' && !document.querySelector('#terminal-output a')`), 'terminal output changed host title or created a link');
  }
  assert(await evaluate(`!document.getElementById('terminal-output').querySelector('img') && !window.pwned`), 'terminal output executed HTML');
  await evaluate(`document.getElementById('terminal-disconnect').click()`);
  await wait(`document.getElementById('terminal-status').textContent === 'Disconnected'`);
  await navigate('logs');
  await evaluate(`document.getElementById("logs-refresh").click()`);
  if (!live) {
    await wait(`document.getElementById("logs").textContent.includes("<img")`);
    assert(await evaluate(`!window.pwned && !document.getElementById("logs").querySelector("img")`),"HTML log executed");
    await setValue('log-stream', 'stderr');
    await setValue('log-resource', 'backend-api-0');
    await setValue('log-container', 'app');
    await evaluate(`document.getElementById("logs-refresh").click()`);
    await wait(`document.getElementById("logs").textContent.includes("log-stderr-marker")`);
    assert(await evaluate(`!document.getElementById("logs").textContent.includes("<img")`),"stream filter ignored");
  } else {
    await wait(`document.getElementById("logs").textContent.includes("live-storage-stdout") && document.getElementById("logs").textContent.includes("live-storage-stderr")`);
    assert(await evaluate(`!window.pwned && !document.getElementById("logs").querySelector("img")`),"real guest HTML log executed");
    await setValue('log-container', 'storage');
    await setValue('log-stream', 'stderr');
    await evaluate(`document.getElementById("logs-refresh").click()`);
    await wait(`document.getElementById("logs").textContent.includes("live-storage-stderr")`);
    assert(await evaluate(`!document.getElementById("logs").textContent.includes("live-storage-stdout")`),"real guest stream filter ignored");
  }
  await navigate('exec');
  if (live) {
    await setValue('exec-target', terminalTarget);
    await setValue('exec-args', JSON.stringify(['/bin/busybox', 'sh', '-c', 'for file in /tmp/grillo-browser-terminal.pid /tmp/grillo-browser-terminal-child.pid; do pid=$(cat "$file"); test -n "$pid" && ! kill -0 "$pid" 2>/dev/null || exit 1; done']));
    await evaluate(`document.getElementById('exec-run').click()`);
    await wait(`document.getElementById('exec-status').textContent === 'Exit code 0'`);
  }
  if (live) await setValue('exec-args', JSON.stringify(["/bin/busybox","sh","-c","echo '<img src=x onerror=window.pwned=true>'; echo stderr-marker >&2; exit 7"]));
  await evaluate(`document.getElementById("exec-run").click()`);
  await wait(`document.getElementById("exec-status").textContent.includes("Exit code 7")`);
  assert(await evaluate(`document.getElementById("exec-stdout").textContent.includes("<img") && document.getElementById("exec-stderr").textContent.trim() === "stderr-marker" && !window.pwned`),"exec output contract/XSS");
  if (live) {
    const commandInput = async args => setValue('exec-args', JSON.stringify(args));
    await commandInput(["/bin/busybox","sh","-c","echo $$ > /tmp/grillo-ui-exec.pid; exec /bin/busybox sleep 300"]);
    await evaluate(`document.getElementById("exec-run").click()`);
    await wait(`document.getElementById("exec-status").textContent === "Running…"`);
    await new Promise(r=>setTimeout(r,750));
    await evaluate(`document.getElementById("exec-cancel").click()`);
    await wait(`document.getElementById("exec-status").textContent === "Request canceled."`);
    await commandInput(["/bin/busybox","sh","-c",'pid=$(cat /tmp/grillo-ui-exec.pid); test -n "$pid" && ! kill -0 "$pid" 2>/dev/null']);
    await evaluate(`document.getElementById("exec-run").click()`);
    await wait(`document.getElementById("exec-status").textContent === "Exit code 0"`);
  }
  await navigate('events');
  if (!live) {
    await wait(`document.getElementById("events").textContent.includes("events.gap")`);
    await wait(`document.getElementById("events").textContent.includes('"seq":3')`);
  }
  // A reconnect after the finite fixture stream must carry Last-Event-ID.
  await setValue('event-filter', 'no-match');
  assert(await evaluate(`document.getElementById("events").textContent === ""`),"event resource filter ignored");
  await navigate('overview');
  assert.deepEqual(await evaluate(`window.__cspErrors`), [], 'PatternFly must not weaken or violate CSP');
  assert.deepEqual(await evaluate(`window.__runtimeErrors`), [], 'browser runtime errors');
  assert(await evaluate(`document.fonts.check('14px "Red Hat Text"')`), 'local PatternFly typography unavailable');
  if (process.env.GRILLO_UI_SCREENSHOT) {
    const shot = await command('browsingContext.captureScreenshot', {context, format:{type:'image/png'}});
    await writeFile(process.env.GRILLO_UI_SCREENSHOT, Buffer.from(shot.data, 'base64'));
  }
  await command('browsingContext.setViewport', {context, viewport:{width:390,height:844}});
  await wait(`document.getElementById('nav-toggle').getAttribute('aria-expanded') === 'false'`);
  await evaluate(`document.getElementById('nav-toggle').click()`);
  await wait(`(() => { const box = document.querySelector('[data-testid="nav-workloads"]').getBoundingClientRect(); return box.width > 0 && box.height > 0 && document.getElementById('page-sidebar').getBoundingClientRect().left >= 0 && box.right <= innerWidth; })()`);
  if (process.env.GRILLO_UI_SCREENSHOT) {
    const shot = await command('browsingContext.captureScreenshot', {context, format:{type:'image/png'}});
    await writeFile(process.env.GRILLO_UI_SCREENSHOT.replace('.png', '-mobile.png'), Buffer.from(shot.data, 'base64'));
  }
  await navigate('workloads');
  assert(await evaluate(`!!document.getElementById('sandboxes')`), 'mobile navigation failed');
  await evaluate(`document.getElementById('nav-toggle').click()`);
  await navigate('topology');
  await evaluate(`document.querySelector('#topology [data-kind="service"]').focus()`);
  await command('input.performActions', {context, actions:[{type:'key', id:'mobile-topology-keyboard', actions:[{type:'keyDown', value:'\uE007'}, {type:'keyUp', value:'\uE007'}]}]});
  await wait(`!!document.getElementById('topology-detail')`);
  assert(await evaluate(`document.getElementById('topology-detail').getBoundingClientRect().width <= innerWidth && document.documentElement.scrollWidth <= innerWidth`), 'mobile topology overflows the document');
  if (process.env.GRILLO_UI_SCREENSHOT) {
    await evaluate(`document.getElementById('topology-detail').scrollIntoView()`);
    const shot = await command('browsingContext.captureScreenshot', {context, format:{type:'image/png'}});
    await writeFile(process.env.GRILLO_UI_SCREENSHOT.replace('.png', '-topology-mobile.png'), Buffer.from(shot.data, 'base64'));
  }
  await setValue('theme-preference', 'dark');
  await wait(`document.documentElement.dataset.theme === 'dark'`);
  assert(await evaluate(`document.documentElement.scrollWidth <= innerWidth && getComputedStyle(document.querySelector('.gr-topology-detail')).backgroundColor === 'rgb(34, 43, 50)'`), 'dark mobile inspector or theme picker overflows');
  await wait(`getComputedStyle(document.querySelector('.gr-resource-actions .pf-m-link')).color === 'rgb(125, 224, 191)'`);
  if (process.env.GRILLO_UI_SCREENSHOT) {
    const shot = await command('browsingContext.captureScreenshot', { context, format: { type: 'image/png' } });
    await writeFile(process.env.GRILLO_UI_SCREENSHOT.replace('.png', '-dark-mobile.png'), Buffer.from(shot.data, 'base64'));
  }
  assert.deepEqual(await evaluate(`window.__cspErrors`), [], 'mobile topology violates CSP');
  assert.deepEqual(await evaluate(`window.__runtimeErrors`), [], 'mobile topology runtime errors');
  await command("browsingContext.close",{context});
  await command("session.end",{});
  console.log(live ? `PASS: real ${browserName} PatternFly React with native daemon/KVM views, local fonts/CSP, real guest logs/filtering/HTML safety, container exec and cancellation without a leaked exec process; desktop/mobile navigation, keyboard topology selection/filter/zoom/detail and closure` : `PASS: real ${browserName} PatternFly React bootstrap, desktop/mobile navigation, local fonts/CSP, views/keyboard topology selection/filter/zoom/detail, metadata-only responses, HTML log safety, exec, SSE gap/reconnect and filters; interactive browser terminal`);
} catch (error) {
  // Bootstrap credentials must not appear in test failure diagnostics.
  throw new Error(String(error.message).split(url).join("[console URL redacted]"));
} finally {
  ws?.close();
  if (!exited) browser.kill("SIGTERM");
  const timer=setTimeout(()=>{if(!exited) browser.kill("SIGKILL");},3000);
  await exitPromise;
  clearTimeout(timer);
  await rm(profile,{recursive:true,force:true});
}
