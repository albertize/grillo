// SPDX-License-Identifier: Apache-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import { readiness, guestMemory, topologyModel, GRAPH_LIMIT } from './presentation.mjs';

test('readiness never calls empty or missing/draining replicas complete', () => {
  assert.equal(readiness({}).complete, false);
  const view = { workloads: [{ replicas: 2 }], sandboxes: [{ ready: true }] };
  assert.deepEqual(readiness(view), { desired: 2, ready: 1, observed: 1, complete: false });
  view.sandboxes.push({ ready: true }); assert.equal(readiness(view).complete, true);
  view.sandboxes[0].draining = true; assert.equal(readiness(view).complete, false);
  view.sandboxes = [{ ready: false }, { ready: 'true' }]; assert.equal(readiness(view).ready, 0);
  assert.equal(readiness({ workloads: [{ replicas: -1 }, { replicas: NaN }] }).desired, 0);
});
test('guest memory is actual guest accounting only; absent/malformed samples are unavailable', () => {
  for (const guest of [undefined, {}, { memoryTotalBytes: 0, memoryAvailableBytes: 0 }, { memoryTotalBytes: 10, memoryAvailableBytes: 11 }, { memoryTotalBytes: 10, memoryAvailableBytes: -1 }, { memoryTotalBytes: Infinity, memoryAvailableBytes: 1 }, { memoryTotalBytes: '10', memoryAvailableBytes: 1 }]) assert.equal(guestMemory(guest), null);
  assert.deepEqual(guestMemory({ memoryTotalBytes: 100, memoryAvailableBytes: 25 }), { total: 100, used: 75, percent: 75 });
  assert.equal(guestMemory({ memoryTotalBytes: 100, memoryAvailableBytes: 100 }).percent, 0);
});
const fixture = () => ({ routes: [{ hostname: 'app.test', path: '/', service: 'web' }, { hostname: 'missing', path: '/', service: 'absent' }], services: [{ name: 'web', workloads: ['frontend', 'frontend', 'absent'] }], workloads: [{ id: 'frontend' }, { id: 'unrelated' }] });
test('topology only connects declared route backends and selectors, deduplicates and rejects dangling targets', () => {
  const model = topologyModel(fixture());
  assert.equal(model.nodes.length, 5); assert.deepEqual(model.edges, [['route:app.test/', 'service:web'], ['service:web', 'workload:frontend']]);
  assert.equal(model.capped, false);
  assert.deepEqual(topologyModel({}), { nodes: [], edges: [], capped: false });
});
test('filters remove edges to hidden resources and retain stable selection keys', () => {
  const filtered = topologyModel(fixture(), ' FRONTEND ');
  assert.equal(filtered.nodes.length, 1); assert.equal(filtered.nodes[0].key, 'workload:frontend'); assert.equal(filtered.edges.length, 0);
  assert.equal(topologyModel(fixture(), '<img onerror=evil>').nodes.length, 0);
  const reordered = fixture(); reordered.workloads.reverse();
  assert.equal(topologyModel(reordered, 'frontend').nodes[0].key, filtered.nodes[0].key);
});
test('graph bounds every resource family; snapshot input remains unchanged', () => {
  const view = fixture(); view.workloads = Array.from({ length: GRAPH_LIMIT + 100 }, (_, i) => ({ id: `work-${i}` }));
  const before = JSON.stringify(view), model = topologyModel(view);
  assert.equal(model.nodes.filter(n => n.kind === 'workload').length, GRAPH_LIMIT); assert.equal(model.capped, true);
  assert.equal(JSON.stringify(view), before);
});
