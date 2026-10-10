// SPDX-License-Identifier: Apache-2.0
// Public snapshot presentation only: no runtime control or traffic inference.
const list = value => Array.isArray(value) ? value : [];
export const GRAPH_LIMIT = 100;

export function podList(view, { query = '', state = '', sort = 'name', descending = false } = {}) {
  const needle = query.trim().toLocaleLowerCase('en');
  const value = pod => String((sort === 'workload' ? pod.workload : sort === 'state' ? pod.state : pod.id) ?? '');
  return list(view?.sandboxes)
    .filter(pod => (!state || pod.state === state) && (!needle || String(pod.id ?? '').toLocaleLowerCase('en').includes(needle)))
    .slice().sort((a, b) => {
      const left = value(a), right = value(b), aid = String(a.id ?? ''), bid = String(b.id ?? '');
      const order = left < right ? -1 : left > right ? 1 : aid < bid ? -1 : aid > bid ? 1 : 0;
      return descending ? -order : order;
    });
}

export function podEvents(records, id) {
  if (!id) return [];
  return list(records).filter(event => event.resource === id || (typeof event.resource === 'string' && event.resource.startsWith(id + '/')));
}

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
  const services = list(view?.services), needle = query.trim().toLocaleLowerCase('en');
  const matching = services.filter(s => String(s.name ?? '').toLocaleLowerCase('en').includes(needle));
  return {
    nodes: matching.slice(0, GRAPH_LIMIT).map(data => ({ key: `service:${String(data.name ?? '')}`, kind: 'service', name: String(data.name ?? ''), data })),
    edges: [],
    capped: matching.length > GRAPH_LIMIT,
  };
}
