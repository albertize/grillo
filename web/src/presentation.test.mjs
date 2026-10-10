// SPDX-License-Identifier: Apache-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import { readiness, guestMemory, topologyModel, GRAPH_LIMIT, podList, podEvents } from './presentation.mjs';

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
test('Pod list filters names and exact statuses, sorts deterministically without mutating snapshots', () => {
  const view = { sandboxes: [{ id: 'api-10', state: 'running', workload: 'api' }, { id: 'api-1', state: 'failed', workload: 'api' }, { id: 'web-0', state: 'running', workload: 'web' }] };
  const before = JSON.stringify(view);
  assert.deepEqual(podList(view).map(p => p.id), ['api-1', 'api-10', 'web-0']);
  assert.deepEqual(podList(view, { query: ' API ', state: 'running' }).map(p => p.id), ['api-10']);
  assert.deepEqual(podList(view, { sort: 'workload', descending: true }).map(p => p.id), ['web-0', 'api-10', 'api-1']);
  assert.deepEqual(podList(view, { sort: 'state' }).map(p => p.id), ['api-1', 'api-10', 'web-0']);
  assert.equal(podList(view, { state: 'Running' }).length, 0);
  assert.equal(podList(view, { query: '<img>' }).length, 0);
  assert.equal(JSON.stringify(view), before);
  assert.deepEqual(podList({}), []);
});
test('Pod events never include similarly named replicas or global/controller events', () => {
  const records = [{ resource: 'api-1' }, { resource: 'api-1/app' }, { resource: 'api-10' }, { resource: 'application/demo' }, { resource: 'workload/api' }, {}, { resource: 123 }];
  assert.deepEqual(podEvents(records, 'api-1'), records.slice(0, 2));
  assert.deepEqual(podEvents(records, ''), []);
  assert.deepEqual(podEvents(undefined, 'api-1'), []);
});
const fixture = () => ({ routes: [{ hostname: 'app.test', path: '/', service: 'web' }, { hostname: 'missing', path: '/', service: 'absent' }], services: [{ name: 'web', workloads: ['frontend', 'frontend', 'absent'] }], workloads: [{ id: 'frontend' }, { id: 'unrelated' }] });
test('topology shows only Services, never routes, workloads, VMs or connections', () => {
  const view = fixture(); view.sandboxes = [{ id: 'frontend-0', workload: 'frontend' }];
  const model = topologyModel(view);
  assert.equal(model.nodes.length, 1); assert.deepEqual(model.edges, []);
  assert.equal(model.nodes[0].kind, 'service');
  assert.equal(model.capped, false);
  assert.deepEqual(topologyModel({}), { nodes: [], edges: [], capped: false });
});
test('filters match only service names and retain stable selection keys', () => {
  const filtered = topologyModel(fixture(), ' WEB ');
  assert.equal(filtered.nodes.length, 1); assert.equal(filtered.nodes[0].key, 'service:web'); assert.equal(filtered.edges.length, 0);
  assert.equal(topologyModel(fixture(), 'frontend').nodes.length, 0);
  assert.equal(topologyModel(fixture(), '<img onerror=evil>').nodes.length, 0);
  const reordered = fixture(); reordered.workloads.reverse();
  assert.equal(topologyModel(reordered, 'web').nodes[0].key, filtered.nodes[0].key);
});
test('overview bounds matching Services after filtering; snapshot input remains unchanged', () => {
  const view = fixture(); view.services = Array.from({ length: GRAPH_LIMIT + 100 }, (_, i) => ({ name: `service-${i}` }));
  const before = JSON.stringify(view), model = topologyModel(view);
  assert.equal(model.nodes.length, GRAPH_LIMIT); assert.equal(model.capped, true);
  const filtered = topologyModel(view, 'service-199');
  assert.equal(filtered.nodes[0].name, 'service-199'); assert.equal(filtered.capped, false);
  assert.equal(JSON.stringify(view), before);
});
