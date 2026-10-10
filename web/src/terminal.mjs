// SPDX-License-Identifier: Apache-2.0
// Scope CSP authorization to styles created by the terminal library, never patch
// the real document or authorize arbitrary inline script/style attributes.
export function terminalDocument(document, nonce) {
  if (!/^[A-Za-z0-9_-]{43}$/.test(nonce || '')) throw new Error('Terminal style authorization unavailable.');
  return new Proxy(document, { get(target, key) {
    if (key === 'createElement') return (name, options) => {
      const element = target.createElement(name, options);
      if (String(name).toLowerCase() === 'style') element.nonce = nonce;
      return element;
    };
    const value = Reflect.get(target, key, target);
    return typeof value === 'function' ? value.bind(target) : value;
  } });
}
export function inputChunks(data) {
  if (new TextEncoder().encode(data).length > 8204) throw new Error('Input exceeds 8 KiB. Paste a smaller selection.');
  const chunks = []; let current = '', size = 0;
  for (const char of data) {
    const bytes = new TextEncoder().encode(char).length;
    if (size + bytes > 8192) { chunks.push(current); current = ''; size = 0; }
    current += char; size += bytes;
  }
  if (current) chunks.push(current);
  return chunks;
}
export function writeTerminal(terminal, bytes, signal) {
  return new Promise(resolve => {
    const done = () => { signal.removeEventListener('abort', done); resolve(); };
    if (signal.aborted) return resolve();
    signal.addEventListener('abort', done, { once: true });
    terminal.write(bytes, done);
  });
}
