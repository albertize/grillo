// SPDX-License-Identifier: Apache-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import { safeRoute, parseCommand, appendEvent, appendLogs, bytes, MAX_TEXT } from './client.mjs';

test('route links allow only actual loopback HTTP, never active or foreign URLs', () => {
  assert.equal(safeRoute('http://127.0.0.1:1234/'), 'http://127.0.0.1:1234/');
  for (const url of ['javascript:alert(1)', 'data:text/html,x', 'https://evil.example', 'http://127.0.0.1.evil.example', 'http://user:password@127.0.0.1/', 'invalid']) assert.equal(safeRoute(url), null);
});
test('commands preserve literal argument boundaries and reject malformed or oversized input', () => {
  assert.deepEqual(parseCommand('["echo", "literal;not-a-host-shell"]'), ['echo', 'literal;not-a-host-shell']);
  for (const input of ['{}', '[]', '[1]', '{', JSON.stringify(Array(129).fill('x')), JSON.stringify(['x'.repeat(32 * 1024)])]) assert.throws(() => parseCommand(input));
});
test('events deduplicate resumed IDs and bound stored text as well as count', () => {
  let state = { records: [], lastSequence: 0 };
  for (let seq = 1; seq < 300; seq++) state = appendEvent(state.records, { seq, kind: 'sandbox.running' }, state.lastSequence);
  assert.equal(state.records.length, 200);
  assert.equal(appendEvent(state.records, { seq: 299 }, state.lastSequence).added, false);
  assert.equal(appendEvent([], { seq: 1, message: 'x'.repeat(MAX_TEXT * 2) }, 0).records.length, 0);
  assert.equal(appendEvent([], { seq: 'invalid' }, 0).added, false);
});
test('log filtering advances all cursor records and caps text', () => {
  const records = [{ seq: 1, stream: 'stdout', line: '<img onerror=alert(1)>' }, { seq: 2, stream: 'stderr', line: 'error-marker' }];
  const result = appendLogs('', 0, records, 'stderr');
  assert.equal(result.since, 2); assert.equal(result.count, 1);
  assert(!result.text.includes('<img')); assert(result.text.includes('error-marker'));
  const gap = appendLogs('', 0, [{ seq: 1, stream: 'stderr', gap: true, line: 'output was lost' }], 'stdout');
  assert.equal(gap.count, 1); assert(gap.text.includes('output was lost'));
  assert.equal(appendLogs(result.text, result.since, records, '').count, 0);
  assert.equal(appendLogs('', 0, [{ seq: 1, line: 'x'.repeat(MAX_TEXT * 2) }], '').text.length, MAX_TEXT);
  const unicode = appendLogs('', 0, [{ seq: 1, line: '😀'.repeat(MAX_TEXT) }], '').text;
  assert(new TextEncoder().encode(unicode).length <= MAX_TEXT);
  assert(!unicode.includes('\ufffd'));
});
test('unavailable metrics never become invented zeroes', () => {
  assert.equal(bytes(undefined), 'Unavailable');
  assert.equal(bytes(512 * 1048576), '512.0 MiB');
});
