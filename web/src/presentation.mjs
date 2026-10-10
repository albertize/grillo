// SPDX-License-Identifier: Apache-2.0
// Public snapshot presentation only: no runtime control or traffic inference.
const list = value => Array.isArray(value) ? value : [];
export const GRAPH_LIMIT = 100;
export function readiness(view) {
  const sandboxes = list(view?.sandboxes);
  const desired = list(view?.workloads).reduce((n, w) => n + (Number.isInteger(w.replicas) && w.replicas > 0 ? w.replicas : 0), 0);
  const ready = sandboxes.filter(s => s.ready === true && !s.draining).length;
  return { desired, ready, observed: sandboxes.length, complete: desired > 0 && ready === desired && ready === sandboxes.length };
}
export function guestMemory(guest) {
  const total = guest?.memoryTotalBytes, available = guest?.memoryAvailableBytes;
  if (!Number.isFinite(total) || !Number.isFinite(available) || total <= 0 || available < 0 || available > total) return null;
  return { total, used: total - available, percent: Math.round((total - available) / total * 100) };
}
export function topologyModel(view, query = '') {
  const groups = [
    ['route', list(view?.routes), r => `${r.hostname}${r.path}`],
    ['service', list(view?.services), s => s.name],
    ['workload', list(view?.workloads), w => w.id],
  ];
  const nodes = groups.flatMap(([kind, items, name], column) => items.slice(0, GRAPH_LIMIT).map(data => ({ key: `${kind}:${String(name(data) ?? '')}`, kind, name: String(name(data) ?? ''), data, column })));
  const services = new Map(nodes.filter(n => n.kind === 'service').map(n => [n.data.name, n]));
  const workloads = new Map(nodes.filter(n => n.kind === 'workload').map(n => [n.data.id, n]));
  const edges = [];
  for (const node of nodes) {
    if (node.kind === 'route') { const target = services.get(node.data.service); if (target) edges.push([node.key, target.key]); }
    if (node.kind === 'service') for (const id of new Set(list(node.data.workloads))) { const target = workloads.get(id); if (target) edges.push([node.key, target.key]); }
  }
  const needle = query.trim().toLocaleLowerCase('en');
  const visible = nodes.filter(n => !needle || n.name.toLocaleLowerCase('en').includes(needle));
  const keys = new Set(visible.map(n => n.key));
  return { nodes: visible, edges: edges.filter(([a, b]) => keys.has(a) && keys.has(b)), capped: groups.some(([, items]) => items.length > GRAPH_LIMIT) };
}
