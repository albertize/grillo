// SPDX-License-Identifier: Apache-2.0
// Opt-in real Firefox WebDriver BiDi smoke test. No packages or downloads.
import { spawn } from "node:child_process";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";
import assert from "node:assert/strict";

const url = process.env.GRILLO_UI_TEST_URL;
const live = process.env.GRILLO_UI_TEST_LIVE === "1";
assert(url, "GRILLO_UI_TEST_URL is required (never print its token)");
const profile = await mkdtemp(join(tmpdir(), "grillo-firefox-"));
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
const browser = spawn("firefox", ["--headless","--no-remote","--profile",profile,"--remote-debugging-port",String(port),"about:blank"], {stdio:["ignore","pipe","pipe"]});
let exited = false, diagnostics = "", ws;
const exitPromise = new Promise(resolve => {
  browser.once("exit",(code,signal)=>{exited=true;resolve({code,signal});});
  browser.once("error",e=>{diagnostics=e.message;exited=true;resolve({error:e.message});});
});
for (const stream of [browser.stdout,browser.stderr]) stream.on("data",chunk=>{diagnostics=(diagnostics+chunk).slice(-8192);});
const pending = new Map(); let nextID = 0;
try {
  for (let i=0;i<100;i++) {
    if (exited) throw new Error("Firefox exited before remote handshake: "+diagnostics);
    try {
      ws = new WebSocket(`ws://127.0.0.1:${port}/session`);
      await new Promise((resolve,reject)=>{
        const timer=setTimeout(()=>reject(new Error("BiDi connection timeout")),500);
        ws.addEventListener("open",()=>{clearTimeout(timer);resolve();},{once:true});
        ws.addEventListener("error",e=>{clearTimeout(timer);reject(e);},{once:true});
      });
      break;
    } catch { ws?.close(); ws=null; await new Promise(r=>setTimeout(r,100)); }
  }
  assert(ws,"Firefox WebDriver BiDi unavailable: "+diagnostics);
  ws.addEventListener("message",event=>{
    const message=JSON.parse(event.data), waiter=pending.get(message.id);
    if (!waiter) return;
    pending.delete(message.id);
    if (message.type==="error") waiter.reject(new Error(message.error+": "+message.message)); else waiter.resolve(message.result);
  });
  function command(method,params) {
    const id=++nextID;
    return new Promise((resolve,reject)=>{
      const timer=setTimeout(()=>{pending.delete(id);reject(new Error("BiDi timeout: "+method));},10000);
      pending.set(id,{resolve:r=>{clearTimeout(timer);resolve(r);},reject:e=>{clearTimeout(timer);reject(e);}});
      ws.send(JSON.stringify({id,method,params}));
    });
  }
  await command("session.new",{capabilities:{}});
  await command("script.addPreloadScript", {functionDeclaration: `() => {
    window.__cspErrors = []; window.__runtimeErrors = [];
    addEventListener('securitypolicyviolation', e => window.__cspErrors.push(e.violatedDirective));
    addEventListener('error', e => window.__runtimeErrors.push(e.message));
  }`});
  const {context}=await command("browsingContext.create",{type:"tab"});
  await command("browsingContext.setViewport", {context, viewport: {width: 1440, height: 1000}});
  await command("browsingContext.navigate",{context,url,wait:"complete"});
  async function evaluate(expression) {
    const r=await command("script.evaluate",{expression:`(async () => JSON.stringify((await (${expression})) ?? null))()`,target:{context},awaitPromise:true});
    if (r.type!=="success") throw new Error("Browser JavaScript exception: "+JSON.stringify(r.exceptionDetails));
    return JSON.parse(r.result.value);
  }
  async function wait(expression) {
    for (let i=0;i<100;i++) { if(await evaluate(expression)) return; await new Promise(r=>setTimeout(r,100)); }
    throw new Error("Browser condition timed out: "+expression+"; status: "+await evaluate(`document.getElementById("exec-status")?.textContent || document.body.textContent.slice(0, 700)`));
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
  await wait(`document.getElementById("overview")?.textContent.includes("observed microVMs")`);
  assert(await evaluate(`location.hash === ""`),"bootstrap fragment not removed");
  assert(await evaluate(`!!document.querySelector('.pf-v6-c-page[data-console="patternfly-react"]')`),"PatternFly React not mounted");
  assert(await evaluate(`document.getElementById("metrics").textContent.length > 0`),"missing metrics");
  if (live) {
    assert(await evaluate(`document.getElementById("metrics").textContent.includes("/proc")`),"no real VMM sample");
    assert(await evaluate(`document.getElementById("metrics").textContent.includes("guest /proc/meminfo") && document.getElementById("metrics").textContent.includes("guest cgroup v2") && document.getElementById("metrics").textContent.includes("µs")`),"no real guest/container usage samples");
  }
  for (const [section, ids] of [['workloads',['workloads','sandboxes']],['networking',['routes']],['storage',['volumes']],['configuration',['configs','secrets']],['diagnostics',['diagnostics']]]) {
    await navigate(section);
    for (const id of ids) assert(await evaluate(`document.getElementById(${JSON.stringify(id)}).textContent.length > 0`),"empty pane: "+id);
  }
  await navigate('topology');
  assert(await evaluate(`document.getElementById("topology").querySelectorAll("rect").length >= 3`),"missing topology");
  await evaluate(`document.querySelector('#topology [data-kind="service"]').focus()`);
  await command('input.performActions', {context, actions:[{type:'key', id:'topology-keyboard', actions:[{type:'keyDown', value:'\uE007'}, {type:'keyUp', value:'\uE007'}]}]});
  await wait(`document.getElementById('topology-detail')?.textContent.includes('Services')`);
  assert(await evaluate(`document.querySelector('#topology [data-kind="service"]').getAttribute('aria-pressed') === 'true'`), 'keyboard selection not exposed');
  assert(await evaluate(`getComputedStyle(document.querySelector('.gr-masthead')).backgroundColor === 'rgb(16, 43, 53)'`), 'Grillo masthead palette missing');
  assert(await evaluate(`getComputedStyle(document.getElementById('refresh')).color === 'rgb(8, 114, 108)'`), 'Grillo action palette missing');
  await evaluate(`Array.from(document.querySelectorAll('#topology-detail [role="tab"]')).find(t => t.textContent === 'Details').click()`);
  await wait(`document.getElementById('topology-detail').textContent.includes('Relationship source')`);
  await evaluate(`Array.from(document.querySelectorAll('#topology-detail [role="tab"]')).find(t => t.textContent === 'Resources').click()`);
  if (process.env.GRILLO_UI_SCREENSHOT) {
    const shot = await command('browsingContext.captureScreenshot', {context, format:{type:'image/png'}});
    await writeFile(process.env.GRILLO_UI_SCREENSHOT.replace('.png', '-topology.png'), Buffer.from(shot.data, 'base64'));
  }
  await evaluate(`document.querySelector('[aria-label="Close resource detail"]').click()`);
  await wait(`!document.getElementById('topology-detail')`);
  assert(await evaluate(`document.activeElement?.getAttribute('data-kind') === 'service'`), 'closing detail loses keyboard focus');
  await setValue('topology-search', 'no-topology-match-unique');
  await wait(`document.querySelectorAll('#topology [role="button"]').length === 0`);
  await setValue('topology-search', '');
  await wait(`document.querySelectorAll('#topology [role="button"]').length >= 3`);
  await evaluate(`document.querySelector('[aria-label="Zoom in"]').click()`);
  assert(await evaluate(`!!document.querySelector('.gr-zoom-in')`), 'zoom control failed');
  await evaluate(`document.querySelector('[aria-label="Fit topology"]').click()`);
  assert(await evaluate(`!document.body.textContent.includes("synthetic-hidden-value") && !document.body.textContent.includes("synthetic-cli-private-token")`),"secret/config value in DOM");
  assert(await evaluate(`!document.cookie.includes("grillo_session")`),"session is not HttpOnly");
  const result=await evaluate(`(async()=>{const r=await fetch("/v1/applications/" + encodeURIComponent(document.getElementById("apps").value) + "/view"); return {status:r.status,body:await r.text()};})()`);
  assert.equal(result.status,200);assert(!result.body.includes("synthetic-hidden-value") && !result.body.includes("synthetic-cli-private-token"),"private value in public response");
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
  assert.deepEqual(await evaluate(`window.__cspErrors`), [], 'mobile topology violates CSP');
  assert.deepEqual(await evaluate(`window.__runtimeErrors`), [], 'mobile topology runtime errors');
  await command("browsingContext.close",{context});
  await command("session.end",{});
  console.log(live ? "PASS: real Firefox PatternFly React with native daemon/KVM views, local fonts/CSP, real guest logs/filtering/HTML safety, container exec and cancellation without a leaked exec process; desktop/mobile navigation, keyboard topology selection/filter/zoom/detail and closure" : "PASS: real Firefox PatternFly React bootstrap, desktop/mobile navigation, local fonts/CSP, views/keyboard topology selection/filter/zoom/detail, metadata-only responses, HTML log safety, exec, SSE gap/reconnect and filters");
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
