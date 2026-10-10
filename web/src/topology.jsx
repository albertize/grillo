// SPDX-License-Identifier: Apache-2.0
import React, { useRef, useState } from 'react';
import { Alert, Button, Card, CardBody, Label, TextInput, Title } from '@patternfly/react-core';
import { MinusIcon, PlusIcon, TimesIcon, ExpandArrowsAltIcon } from '@patternfly/react-icons';
import { topologyModel } from './presentation.mjs';
import { safeRoute } from './client.mjs';
const list = value => Array.isArray(value) ? value : [];

function Detail({ node, view, openLogs, openTTY, close }) {
  const service = node.data;
  const pods = list(view.sandboxes).filter(s => list(service.workloads).includes(s.workload));
  const routes = list(view.routes).filter(r => r.service === service.name);
  return <aside className="gr-topology-detail" aria-label="Selected service" id="topology-detail">
    <div className="gr-detail-heading"><div><span className="gr-kind gr-kind-service">S</span><Title headingLevel="h2" size="lg">{node.name}</Title></div><Button variant="plain" aria-label="Close resource detail" onClick={close}><TimesIcon /></Button></div>
    <div className="gr-detail-body"><dl className="gr-detail-list"><dt>Discovery</dt><dd>{service.headless ? 'Headless' : 'ClusterIP'}</dd><dt>Declared TCP ports</dt><dd>{list(service.ports).join(', ') || 'None'}</dd><dt>Selected workloads</dt><dd>{list(service.workloads).join(', ') || 'None'}</dd></dl>
      <section><h3>Instances / Pods <Label isCompact>{pods.length}</Label></h3>{pods.length ? pods.slice(0, 100).map(pod => <div className="gr-resource-row" key={pod.id}><strong>{pod.id}</strong><p>Workload: {pod.workload}</p><p>Private IP: {pod.ip || 'Unavailable'}</p><Label color={pod.ready && !pod.draining ? 'green' : 'grey'} isCompact>{pod.draining ? 'Draining' : pod.ready ? 'Ready' : pod.state || 'Unavailable'}</Label><div className="gr-resource-actions"><Button variant="link" isInline onClick={() => openLogs(pod.id)}>Logs</Button><Button variant="link" isInline onClick={() => openTTY(pod.id)}>Terminal</Button></div></div>) : <p className="gr-muted">No observed Pods for this Service.</p>}{pods.length > 100 && <p className="gr-muted">Showing the first 100 observations.</p>}<p className="gr-muted">Instances are selected by declared Service selectors, not traffic telemetry.</p></section>
      <section><h3>Published routes</h3>{routes.map((r, i) => <div className="gr-resource-row" key={i}><strong>{r.hostname}{r.path}</strong><p>{safeRoute(r.endpoint) ? <a href={safeRoute(r.endpoint)} target="_blank" rel="noopener noreferrer">{r.endpoint}</a> : 'Endpoint unavailable'}</p></div>)}{!routes.length && <p className="gr-muted">No declared routes.</p>}</section>
    </div>
  </aside>;
}
export function Topology({ view, openLogs, openTTY }) {
  const [query, setQuery] = useState(''), [selected, setSelected] = useState(''), [zoom, setZoom] = useState(1);
  const nodeElements = useRef(new Map());
  const model = topologyModel(view, query), node = model.nodes.find(n => n.key === selected);
  function closeDetail() { nodeElements.current.get(selected)?.focus(); setSelected(''); }
  const height = Math.max(450, Math.ceil(model.nodes.length / 3) * 135 + 70);
  return <div className="gr-topology-shell">
    <div className="gr-topology-toolbar"><div><Title headingLevel="h2" size="md">Services overview</Title><span className="gr-muted">{model.nodes.length} Services · select for instance information</span></div><TextInput id="topology-search" maxLength={256} aria-label="Find topology resource by name" placeholder="Find Service by name…" value={query} onChange={(_, value) => { setQuery(value); setSelected(''); }} /><Button variant="secondary" isDisabled={!query} onClick={() => setQuery('')}>Clear filter</Button></div>
    {model.capped && <Alert isInline variant="info" title="Overview limited to 100 matching Services" />}
    <div className={`gr-topology-workspace${node ? ' gr-topology-selected' : ''}`}><div className="gr-topology-canvas"><div className={`gr-topology gr-zoom-${zoom === 1 ? 'fit' : zoom < 1 ? 'out' : 'in'}`}><svg id="topology" viewBox={`0 0 960 ${height}`} role="group" aria-label="Selectable Services overview">
      {model.nodes.map((n, i) => { const x = 35 + i % 3 * 310, y = 90 + Math.floor(i / 3) * 135; return <g key={n.key} ref={element => { if (element) nodeElements.current.set(n.key, element); else nodeElements.current.delete(n.key); }} role="button" tabIndex="0" data-kind="service" aria-label={`Service ${n.name}`} aria-pressed={selected === n.key} className={`gr-graph-node gr-node-service${selected === n.key ? ' gr-node-selected' : ''}`} onClick={() => setSelected(n.key)} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setSelected(n.key); } }}>
        <title>{n.name} · Service</title><rect x={x} y={y - 48} width="245" height="108" rx="3" /><circle className="gr-node-ring" cx={x + 36} cy={y - 7} r="22" /><text x={x + 36} y={y - 2} textAnchor="middle" className="gr-node-symbol">S</text><text x={x + 70} y={y - 10} className="gr-graph-name">{n.name.length > 21 ? n.name.slice(0, 20) + '…' : n.name}</text><text x={x + 70} y={y + 11} className="gr-graph-detail">{n.data.headless ? 'Headless' : 'ClusterIP'}</text><text x={x + 14} y={y + 42} className="gr-graph-detail">TCP ports: {list(n.data.ports).join(', ') || 'None'}</text>
      </g>; })}
    </svg>{!model.nodes.length && <div className="gr-topology-empty">No Services match this view.</div>}</div><div className="gr-canvas-controls"><Button variant="secondary" aria-label="Zoom out" isDisabled={zoom === 0.75} onClick={() => setZoom(0.75)}><MinusIcon /></Button><Button variant="secondary" aria-label="Zoom in" isDisabled={zoom === 1.25} onClick={() => setZoom(1.25)}><PlusIcon /></Button><Button variant="secondary" aria-label="Fit topology" onClick={() => setZoom(1)}><ExpandArrowsAltIcon /></Button><span>Services only · no connections</span></div></div>
    {node ? <Detail key={node.key} node={node} view={view} openLogs={openLogs} openTTY={openTTY} close={closeDetail} /> : <Card className="gr-topology-prompt"><CardBody><h3>Explore your Services</h3><p>Select a Service to inspect its information and observed Pod instances.</p><p>Pod instances appear in the inspector, not in the overview.</p></CardBody></Card>}
    </div><p className="gr-topology-caption">Keyboard: Tab to a Service, Enter or Space to inspect. No connections or traffic telemetry.</p>
  </div>;
}
