// SPDX-License-Identifier: Apache-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import { terminalDocument, inputChunks, writeTerminal } from './terminal.mjs';
test('terminal style nonce is scoped to the document adapter, not globals or scripts', () => {
  const nonce = 'n'.repeat(43), original = { createElement: name => ({ name }), value: 3, getValue() { assert.equal(this, original); return this.value; } };
  const scoped = terminalDocument(original, nonce);
  assert.equal(scoped.createElement('style').nonce, nonce); assert.equal(scoped.createElement('script').nonce, undefined);
  assert.equal(original.createElement('style').nonce, undefined); assert.equal(scoped.getValue(), 3);
  for (const bad of ['', '" unsafe-inline', undefined]) assert.throws(() => terminalDocument(original, bad));
});
test('terminal input bounds paste and preserves UTF-8 when splitting bracketed paste', () => {
  const data = '😀'.repeat(2048) + 'abcdefghijkl';
  const chunks = inputChunks(data); assert.equal(chunks.length, 2); assert.equal(chunks.join(''), data);
  assert(chunks.every(chunk => new TextEncoder().encode(chunk).length <= 8192));
  assert.deepEqual(inputChunks('\x03'), ['\x03']); assert.deepEqual(inputChunks(''), []);
  assert.throws(() => inputChunks('x'.repeat(8205)));
});
test('terminal parser backpressure waits for writes and releases on cancellation', async () => {
  const controller = new AbortController(); let callback, written, finished = false;
  const term = { write: (bytes, done) => { written = bytes; callback = done; } };
  const bytes = new Uint8Array([0, 255]); const pending = writeTerminal(term, bytes, controller.signal).then(() => { finished = true; });
  await Promise.resolve(); assert.equal(finished, false); assert.equal(written, bytes);
  callback(); await pending; assert.equal(finished, true);
  const canceled = writeTerminal(term, bytes, controller.signal); controller.abort(); await canceled;
  await writeTerminal({ write() { assert.fail('write after cancellation'); } }, bytes, controller.signal);
});
