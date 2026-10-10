// SPDX-License-Identifier: Apache-2.0
import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, FormGroup, FormSelect, FormSelectOption } from '@patternfly/react-core';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { terminalDocument, inputChunks, writeTerminal } from './terminal.mjs';
const list = value => Array.isArray(value) ? value : [];
async function post(path, body, options = {}) {
  const response = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), ...options });
  if (!response.ok) throw new Error(`Terminal request failed (HTTP ${response.status}).`);
  return response;
}
export function PodTerminal({ view, pod, preferredContainer = '' }) {
  const containers = list(pod.containers).filter(c => c.state === 'running');
  const [container, setContainer] = useState(preferredContainer || containers[0]?.name || ''), [status, setStatus] = useState('Disconnected'), [busy, setBusy] = useState(false);
  const active = useRef(null), terminal = useRef(null), area = useRef(null), fit = useRef(null);
  const dimensions = useRef({ rows: 24, cols: 80 });
  function stop() {
    const session = active.current; active.current = null;
    if (terminal.current) terminal.current.options.disableStdin = true;
    if (session?.id) post(`/v1/terminal/${session.id}/close`, {}, { keepalive: true }).catch(() => {});
    session?.controller.abort();
  }
  function send(body) {
    const session = active.current;
    if (!session?.id) return;
    if (session.pending >= 16) { setStatus('Input queue exceeded; terminal disconnected to avoid losing keystrokes.'); stop(); setBusy(false); return; }
    session.pending++;
    session.queue = session.queue.then(async () => {
      if (active.current !== session) return;
      await post(`/v1/terminal/${session.id}/input`, body, { signal: AbortSignal.any([session.controller.signal, AbortSignal.timeout(5000)]) });
    }).catch(error => { if (active.current === session) { setStatus(error.message); stop(); setBusy(false); } }).finally(() => { session.pending--; });
  }
  useEffect(() => {
    const nonce = document.querySelector('meta[name="terminal-style-nonce"]')?.content;
    const term = new Terminal({ documentOverride: terminalDocument(document, nonce), allowProposedApi: true,
      disableStdin: true, cursorBlink: true, scrollback: 1000, fontFamily: '"Red Hat Mono", monospace', fontSize: 13,
      theme: { background: '#10232d', foreground: '#d5e7ec', cursor: '#7de0bf', selectionBackground: '#30525c' },
      windowOptions: {}, linkHandler: { activate() {} } });
    const fitter = new FitAddon(); term.loadAddon(fitter);
    // No workload-driven host title, link, clipboard or window effects.
    const subscriptions = [0, 1, 2, 8, 52].map(code => term.parser.registerOscHandler(code, () => true));
    subscriptions.push(term.onData(data => {
      try { for (const chunk of inputChunks(data)) send({ data: chunk }); }
      catch (error) { setStatus(error.message); }
    }), term.onBinary(data => { if (data.length <= 8192) send({ bytes: btoa(data) }); }), term.onResize(({ rows, cols }) => { dimensions.current = { rows, cols }; send({ rows, cols }); }));
    terminal.current = term;
    term.open(area.current);
    term.textarea.id = 'terminal-input'; term.textarea.setAttribute('aria-label', 'Interactive container terminal');
    const fitTerminal = () => {
      const size = fitter.proposeDimensions(); if (!size) return;
      term.resize(Math.max(1, Math.min(500, size.cols)), Math.max(1, Math.min(300, size.rows)));
    };
    fit.current = fitTerminal; fitTerminal();
    const observer = new ResizeObserver(fitTerminal); observer.observe(area.current);
    let disposed = false;
    document.fonts.ready.then(() => { if (!disposed) fitTerminal(); });
    const pasteGuard = event => {
      if (new TextEncoder().encode(event.clipboardData?.getData('text') || '').length > 8192) {
        event.preventDefault(); event.stopImmediatePropagation(); setStatus('Paste exceeds 8 KiB. Paste a smaller selection.');
      }
    };
    area.current.addEventListener('paste', pasteGuard, true);
    const element = area.current, close = () => stop(); window.addEventListener('pagehide', close);
    return () => { disposed = true; stop(); observer.disconnect(); window.removeEventListener('pagehide', close); element.removeEventListener('paste', pasteGuard, true); subscriptions.forEach(s => s.dispose()); term.dispose(); terminal.current = null; fit.current = null; };
  }, []);
  async function connect() {
    if (!containers.some(c => c.name === container) || active.current || !terminal.current) return;
    const session = { controller: new AbortController(), id: '', queue: Promise.resolve(), pending: 0, received: 0 };
    active.current = session; setBusy(true); setStatus('Connecting…'); terminal.current.reset(); fit.current?.();
    const textDecoder = new TextDecoder(); let receivedExit = false;
    try {
      const response = await post('/v1/terminal', { application: view.application, container: `${pod.id}/${container}`, ...dimensions.current }, { signal: session.controller.signal });
      const reader = response.body.getReader(); let pending = '';
      while (active.current === session) {
        const { value, done } = await reader.read(); if (done) break;
        pending += textDecoder.decode(value, { stream: true });
        let boundary;
        while ((boundary = pending.indexOf('\n\n')) !== -1 && active.current === session) {
          const frame = pending.slice(0, boundary); pending = pending.slice(boundary + 2);
          if (frame.length > 16384 || !frame.startsWith('data: ')) throw new Error('Invalid terminal stream.');
          const message = JSON.parse(frame.slice(6));
          if (message.type === 'session') {
            if (session.id || typeof message.id !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(message.id)) throw new Error('Invalid terminal session.');
            session.id = message.id; setStatus('Connected'); terminal.current.options.disableStdin = false; terminal.current.focus(); send(dimensions.current);
          } else if (message.type === 'output') {
            if (typeof message.data !== 'string' || message.data.length > 12000) throw new Error('Invalid terminal output size.');
            const bytes = Uint8Array.from(atob(message.data), ch => ch.charCodeAt(0)); session.received += bytes.length;
            if (bytes.length > 8192 || session.received > 16 * 1024 * 1024) throw new Error('Terminal output limit exceeded.');
            await writeTerminal(terminal.current, bytes, session.controller.signal);
          } else if (message.type === 'exit') { receivedExit = true; setStatus(`Exit code ${message.exitCode}`); }
          else if (message.type === 'error') { receivedExit = true; setStatus(message.message); }
          else throw new Error('Invalid terminal message.');
        }
        if (pending.length > 16384) throw new Error('Terminal frame exceeds the display limit.');
      }
      if (active.current === session && !receivedExit) setStatus('Connection ended without an exit result.');
    } catch (error) { if (active.current === session && error.name !== 'AbortError') setStatus(error.message); }
    finally { if (active.current === session) { stop(); setBusy(false); } }
  }
  return <div className="gr-stack"><FormGroup label="Container" fieldId="tty-container"><FormSelect id="tty-container" aria-label="Terminal container" value={container} isDisabled={busy} onChange={(_, value) => setContainer(value)}>{containers.length ? containers.map(c => <FormSelectOption key={c.name} value={c.name} label={c.name} />) : <FormSelectOption value="" label="No running containers" />}</FormSelect></FormGroup>
    <div className="gr-actions"><Button id="terminal-connect" onClick={connect} isDisabled={busy || !containers.some(c => c.name === container)}>Connect</Button><Button id="terminal-disconnect" variant="secondary" isDisabled={!busy} onClick={() => { stop(); setBusy(false); setStatus('Disconnected'); }}>Disconnect</Button><span id="terminal-status" role="status">{status}</span></div>
    <div ref={area} id="terminal-output" className="gr-output gr-terminal-output" />
    <Alert isInline variant="info" title="Interactive container terminal">Type directly in the terminal. Ctrl+C interrupts; Ctrl+D sends EOF. Disconnecting or leaving this tab cancels this shell, not the Pod. The image must contain /bin/sh.</Alert>
    <p className="gr-muted">xterm-compatible PTY with ANSI colors, cursor navigation and alternate screen. Scrollback: 1000 lines; paste: 8 KiB; session: 30 minutes / 16 MiB. Workload output is not secret-redacted. Host clipboard requests and output-driven links/window changes are disabled.</p>
  </div>;
}
