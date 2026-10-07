// SPDX-License-Identifier: Apache-2.0
export const MAX_TEXT = 256 * 1024;
const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', { fatal: true });
function tailText(value) {
  const data = encoder.encode(value);
  if (data.length <= MAX_TEXT) return value;
  let start = data.length - MAX_TEXT;
  while ((data[start] & 0xc0) === 0x80) start++;
  return decoder.decode(data.subarray(start));
}

export async function request(path, options) {
  const response = await fetch(path, options);
  if (!response.ok) throw new Error(`Request failed (HTTP ${response.status}). Check the daemon or reopen the console URL if unauthorized.`);
  return response.json();
}

export async function bootstrapSession() {
  const token = new URLSearchParams(location.hash.slice(1)).get('token');
  history.replaceState(null, '', location.pathname + location.search);
  if (!token) return;
  const response = await fetch('/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) });
  if (!response.ok) throw new Error('Session rejected or expired. Restart grillo ui and open its new URL.');
}

export function safeRoute(endpoint) {
  try {
    const url = new URL(endpoint);
    return url.protocol === 'http:' && url.hostname === '127.0.0.1' && !url.username && !url.password ? url.href : null;
  } catch { return null; }
}

export function parseCommand(input) {
  let args;
  try { args = JSON.parse(input); } catch { throw new Error('Use a JSON argument array, for example ["/bin/busybox", "id"].'); }
  if (!Array.isArray(args) || !args.length || args.length > 128 || !args.every(a => typeof a === 'string')) throw new Error('Supply between 1 and 128 string arguments.');
  if (new TextEncoder().encode(JSON.stringify(args)).length > 30 * 1024) throw new Error('Command exceeds the request size limit.');
  return args;
}

export function appendEvent(records, event, lastSequence) {
  if (!event || typeof event !== 'object' || !Number.isSafeInteger(event.seq) || event.seq <= lastSequence) return { records, lastSequence, added: false };
  const eventSize = encoder.encode(JSON.stringify(event)).length;
  if (eventSize > MAX_TEXT - 2) return { records, lastSequence: event.seq, added: true, dropped: true };
  const next = [...records, event];
  const sizes = next.map(record => encoder.encode(JSON.stringify(record)).length);
  let total = sizes.reduce((sum, size) => sum + size + 1, 2);
  while (next.length > 200 || total > MAX_TEXT) { next.shift(); total -= sizes.shift() + 1; }
  return { records: next, lastSequence: event.seq, added: true };
}

export function appendLogs(text, since, records, stream) {
  let next = text, count = 0, cursor = since;
  for (const record of records || []) {
    if (!Number.isSafeInteger(record.seq) || record.seq <= cursor) continue;
    cursor = record.seq;
    if (stream && record.stream !== stream && !record.gap) continue;
    next = tailText(next + `${record.time || ''} ${record.resource}/${record.container || ''} [${record.stream}] ${record.line}\n`);
    count++;
  }
  return { text: next, since: cursor, count };
}

export function bytes(value) {
  return value == null ? 'Unavailable' : `${(value / 1048576).toFixed(1)} MiB`;
}
