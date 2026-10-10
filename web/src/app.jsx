// SPDX-License-Identifier: Apache-2.0
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Alert, Badge, Button, Card, CardBody, CardHeader, CardTitle, Checkbox,
  DescriptionList, DescriptionListDescription, DescriptionListGroup, DescriptionListTerm,
  EmptyState, EmptyStateBody, Form, FormGroup, FormSelect, FormSelectOption,
  Grid, GridItem, Label, Masthead, MastheadBrand, MastheadContent, MastheadLogo,
  MastheadMain, MastheadToggle, Nav, NavGroup, NavItem, NavList, Page, PageSection,
  PageSidebar, PageSidebarBody, PageToggleButton, Spinner, Tab, Tabs, TabTitleText,
  TextArea, TextInput, Title, Toolbar, ToolbarContent, ToolbarItem,
} from '@patternfly/react-core';
import { Table, Thead, Tbody, Tr, Th, Td } from '@patternfly/react-table';
import {
  BarsIcon, CheckCircleIcon, CogIcon, CubesIcon, DatabaseIcon,
  ExclamationCircleIcon, ExternalLinkAltIcon, EyeSlashIcon, NetworkWiredIcon,
  ProjectDiagramIcon, ServerIcon, StreamIcon, SyncAltIcon, TerminalIcon, ThLargeIcon,
} from '@patternfly/react-icons';
import '@patternfly/react-core/dist/styles/base.css';
import './style.css';
import './theme.css';
import { appendEvent, appendLogs, bootstrapSession, bytes, parseCommand, request, safeRoute } from './client.mjs';
import { Topology } from './topology.jsx';
import { readiness, guestMemory, podList, podEvents } from './presentation.mjs';
import { PodTerminal } from './terminal.jsx';
import { ThemePicker } from './theme.jsx';

// Outside React effects: StrictMode must never exchange a monouse token twice.
const startup = bootstrapSession().then(() => null, error => error);
const navigation = [
  ['Home', [['overview', 'Overview', ThLargeIcon], ['topology', 'Topology', ProjectDiagramIcon], ['events', 'Events', StreamIcon]]],
  ['Workloads', [['workloads', 'Pods', CubesIcon], ['controllers', 'Workload controllers', CubesIcon], ['configuration', 'ConfigMaps', CogIcon], ['secrets', 'Secrets', EyeSlashIcon]]],
  ['Networking', [['networking', 'Services', NetworkWiredIcon], ['routes', 'Routes', NetworkWiredIcon]]],
  ['Storage', [['storage', 'Volumes', DatabaseIcon]]],
  ['Observe', [['metrics', 'Metrics', ThLargeIcon], ['logs', 'Logs', StreamIcon], ['exec', 'Exec', TerminalIcon], ['diagnostics', 'Diagnostics', ExclamationCircleIcon]]],
];
const sections = navigation.flatMap(([, pages]) => pages);
// PatternFly 6.6.1's XL breakpoint is 75rem (1200px).
const MOBILE_WIDTH = 1200;
const help = {
  overview: 'Your application at a glance. Desired state, Pod readiness and recent activity.',
  workloads: 'Browse Pods and inspect their containers, logs, events and metrics.',
  controllers: 'Desired workload replicas and container templates. Only actual declared kinds are shown.',
  secrets: 'Secret metadata only. Values remain hidden.',
  routes: 'Published application endpoints and their Service backends.',
  metrics: 'Actual Pod and container samples. Missing metrics are unavailable, never zero.',
  topology: 'Services at a glance. Select a Service to inspect its Pod instances.',
  networking: 'Service discovery and published application endpoints. Management stays separate.',
  storage: 'Persistent, ephemeral and explicitly shared storage attached to this application.',
  configuration: 'ConfigMap metadata only. Contents remain outside this public view.',
  logs: 'Read the daemon’s retained log spool, with resource, container and stream filters.',
  events: 'Global daemon activity. Reconnect resumes from the last event ID; gaps refresh snapshots.',
  exec: 'Run an explicit, non-interactive command inside a selected container. Never on the host.',
  diagnostics: 'Runtime availability and source provenance. Unsupported data is not silently invented.',
};
const list = value => Array.isArray(value) ? value : [];
const running = view => list(view?.sandboxes).flatMap(s => list(s.containers).filter(c => c.state === 'running').map(c => ({ ...c, target: `${s.id}/${c.name}` })));
function Phase({ state }) { return <Label color={state === 'running' || state === 'Ready' ? 'green' : state === 'failed' ? 'red' : state === 'draining' ? 'orange' : 'grey'} isCompact>{state || 'Unavailable'}</Label>; }
function Note({ children }) { return <p className="gr-muted">{children}</p>; }
function Blank({ title = 'No data available', children }) { return <EmptyState titleText={title} headingLevel="h3" icon={ServerIcon}><EmptyStateBody>{children}</EmptyStateBody></EmptyState>; }
function Details({ items }) { return <DescriptionList isHorizontal>{items.map(([term, value]) => <DescriptionListGroup key={term}><DescriptionListTerm>{term}</DescriptionListTerm><DescriptionListDescription>{value ?? 'Unavailable'}</DescriptionListDescription></DescriptionListGroup>)}</DescriptionList>; }
function DataTable({ label, columns, rows }) {
  return rows.length ? <Table aria-label={label} variant="compact"><Thead><Tr>{columns.map(c => <Th key={c}>{c}</Th>)}</Tr></Thead><Tbody>{rows.map((cells, i) => <Tr key={i}>{cells.map((cell, j) => <Td key={j} dataLabel={columns[j]}>{cell ?? 'Unavailable'}</Td>)}</Tr>)}</Tbody></Table> : <Blank title={`No ${label.toLowerCase()}`}>There are no retained or observed resources in this view.</Blank>;
}
function Panel({ title, children, id, action }) { return <Card id={id}><CardHeader actions={action ? { actions: action } : undefined}><CardTitle>{title}</CardTitle></CardHeader><CardBody>{children}</CardBody></Card>; }
function RouteLink({ route }) {
  const href = safeRoute(route.endpoint);
  return href ? <a href={href} target="_blank" rel="noopener noreferrer">{route.endpoint} <ExternalLinkAltIcon /></a> : <span className="gr-muted">Endpoint unavailable</span>;
}
function Metrics({ view }) {
  return <Panel title="Pod metrics" id="metrics"><DataTable label="Container cgroup samples" columns={['Pod / container', 'Memory (cgroup)', 'CPU usage (cumulative)', 'Collected / source']} rows={list(view.sandboxes).flatMap(s => list(s.containers).map(c => [`${s.id}/${c.name}`, bytes(c.usage?.memoryBytes), c.usage?.cpuUsec !== undefined ? `${c.usage.cpuUsec} µs` : 'Unavailable', c.usage ? `${new Date(c.usage.time).toLocaleTimeString()} · ${c.usage.source}` : 'Unavailable']))} /><Note>Container usage comes from actual cgroup samples. CPU percentages and historical charts are unavailable; cumulative CPU time is not a percentage. Missing samples are unavailable, not zero.</Note>
    <details className="gr-advanced"><summary>Runtime accounting (advanced)</summary><DataTable label="Pod environment samples" columns={['Pod', 'Environment total', 'Environment available', 'Busy / idle USER_HZ ticks', 'Collected / source']} rows={list(view.sandboxes).map(s => [s.id, bytes(s.guest?.memoryTotalBytes), bytes(s.guest?.memoryAvailableBytes), s.guest ? `${s.guest.cpuBusyTicks ?? 'Unavailable'} / ${s.guest.cpuIdleTicks ?? 'Unavailable'} ticks` : 'Unavailable', s.guest ? `${new Date(s.guest.time).toLocaleTimeString()} · ${s.guest.source}` : 'Unavailable'])} /><DataTable label="VMM resource samples" columns={['Pod', 'Allocated vCPU', 'Environment memory budget', 'VMM RSS', 'CPU time (/proc)']} rows={list(view.sandboxes).map(s => [s.id, s.vcpu, bytes(s.guestBudgetBytes), bytes(s.vmm?.rssBytes), s.vmm ? `${s.vmm.cpuTimeMs} ms · ${new Date(s.vmm.time).toLocaleTimeString()}` : 'Unavailable'])} /><Note>Runtime environment budget is allocation, not usage. VMM RSS, guest/container actual usage and cache are different accounting domains and are not summed. PSS remains unavailable. Guest-reported data is not host attestation.</Note></details>
  </Panel>;
}
function MemoryGauge({ vm }) {
  const memory = guestMemory(vm.guest);
  return <div className="gr-memory-sample"><svg viewBox="0 0 120 120" role="img" aria-label={memory ? `${vm.id}: ${memory.percent}% guest memory used` : `${vm.id}: guest memory unavailable`}><circle className="gr-gauge-track" cx="60" cy="60" r="45" />{memory && <circle className="gr-gauge-fill" cx="60" cy="60" r="45" pathLength="100" strokeDasharray={`${memory.percent} ${100 - memory.percent}`} transform="rotate(-90 60 60)" />}<text x="60" y="58" textAnchor="middle" className="gr-gauge-value">{memory ? `${memory.percent}%` : '—'}</text><text x="60" y="77" textAnchor="middle" className="gr-gauge-caption">{memory ? 'used' : 'unavailable'}</text></svg><strong>{vm.id}</strong><Note>{memory ? `${bytes(memory.used)} used of ${bytes(memory.total)}` : 'No valid guest memory sample'}</Note>{vm.guest && <Note>{new Date(vm.guest.time).toLocaleTimeString()} · {vm.guest.source}</Note>}</div>;
}
function Overview({ view, navigate, events }) {
  const vms = list(view.sandboxes), status = readiness(view);
  const inventory = [
    ['Workload controllers', list(view.workloads).length, 'controllers', CubesIcon],
    ['Pods', vms.length, 'workloads', CubesIcon],
    ['Running containers', running(view).length, 'workloads', CubesIcon],
    ['Services', list(view.services).length, 'networking', NetworkWiredIcon],
    ['Volumes', list(view.volumes).length, 'storage', DatabaseIcon],
  ];
  return <div className="gr-stack" id="overview"><div className="gr-dashboard">
    <div className="gr-dashboard-column"><Panel title="Application details"><dl className="gr-detail-list"><dt>Application</dt><dd>{view.application}</dd><dt>Source</dt><dd id="source">{view.sourceKind || 'Unavailable'}<p className="gr-muted">{view.sourcePath || 'Source path unavailable'}</p></dd><dt>Scope</dt><dd>Local application</dd><dt>Snapshot</dt><dd>{new Date(view.time).toLocaleString()}</dd></dl></Panel><Panel title="Resource inventory">{inventory.map(([name, count, section, Icon]) => <div className="gr-inventory-row" key={name}><span><Icon /> {name}</span><Button variant="link" isInline onClick={() => navigate(section)} aria-label={`View ${name.toLowerCase()}`}>{count}</Button></div>)}</Panel><Panel title="Diagnostics" action={<Button variant="link" isInline onClick={() => navigate('diagnostics')}>View all</Button>}><strong className="gr-summary-number">{list(view.diagnostics).length}</strong><Note>Reported diagnostics. A zero count is not proof of health.</Note></Panel></div>
    <div className="gr-dashboard-column"><Panel title="Workload readiness" action={<Button variant="link" isInline onClick={() => navigate('topology')}>Topology</Button>}><div className="gr-readiness"><CheckCircleIcon className={status.complete ? 'gr-ready' : 'gr-muted'} /><div><strong>{status.observed ? `${status.ready}/${status.observed} observed Pods ready` : 'No Pods observed'}</strong><Note>{status.desired} desired replicas · {status.complete ? 'Observed readiness matches desired count' : 'Readiness incomplete or no desired replicas'}</Note></div></div></Panel><Panel title="Pod environment memory"><div className="gr-gauges">{vms.slice(0, 4).map(vm => <MemoryGauge key={vm.id} vm={vm} />)}</div>{!vms.length && <Note>No observed Pods to sample.</Note>}<Note>Pod environment memory, not container usage or host capacity. {vms.length > 4 && 'Showing first four observations.'} CPU percentage, host disk and network utilization are unavailable.</Note></Panel><Panel title="Published routes" id="routes" action={<Button variant="link" isInline onClick={() => navigate('routes')}>View all</Button>}>{list(view.routes).length ? list(view.routes).map((r, i) => <div className="gr-route" key={i}><strong>{r.hostname}{r.path}</strong><Note>Service {r.service}</Note><RouteLink route={r} /></div>) : <Note>No published routes.</Note>}</Panel></div>
    <div className="gr-dashboard-column"><Panel title="Recent activity" action={<Button variant="link" isInline onClick={() => navigate('events')}>View all</Button>}>{events.length ? events.slice(-6).reverse().map(e => <div className="gr-activity" key={e.seq}><Label color="teal" isCompact>{e.kind}</Label><Note>{e.resource || 'Daemon'}</Note><p>{e.message || 'No message'}</p></div>) : <Note>No retained daemon events. Sparse activity does not imply a healthy or silent workload.</Note>}<Note>Global daemon events, not application-only activity.</Note></Panel><Panel title="Pods"><Note>Inspect your application’s running instances and containers. Closing this console leaves workloads running.</Note><Button variant="link" isInline onClick={() => navigate('workloads')}>View Pods</Button></Panel></div>
    </div></div>;
}
function Controllers({ view }) {
  return <Panel title="Workload controllers" id="controllers"><DataTable label="Workload controllers" columns={['Name', 'Kind', 'Desired replicas', 'Containers']} rows={list(view.workloads).map(w => [w.id, <Label color="blue">{w.kind}</Label>, w.replicas, list(w.containers).map(c => <Label key={c.name} color={c.init ? 'grey' : 'teal'}>{c.name}{c.init ? ' · init' : ''}</Label>)])} /><Note>Declared Grillo workloads, not synthetic Kubernetes controllers or ReplicaSets. Scaling and rollout actions are not available in this console.</Note></Panel>;
}
function Workloads({ view, openExec, records, connection, gaps, preferredPod = '', preferredMode = '' }) {
  const [selected, setSelected] = useState(preferredPod), [tab, setTab] = useState(preferredMode === 'tty' ? 'terminal' : preferredMode || 'details');
  const [query, setQuery] = useState(''), [state, setState] = useState(''), [sort, setSort] = useState('name'), [descending, setDescending] = useState(false), [activeContainer, setActiveContainer] = useState('');
  function inspect(id, nextTab = 'details') { setSelected(id); setTab(nextTab); setActiveContainer(''); }
  const pods = podList(view, { query, state, sort, descending }), pod = list(view.sandboxes).find(s => s.id === selected);
  const workload = list(view.workloads).find(w => w.id === pod?.workload);
  if (selected) return <div className="gr-stack" id="workloads">
    <div className="gr-resource-breadcrumb"><Button variant="link" isInline onClick={() => setSelected('')}>Pods</Button><span>/</span><span>{selected}</span></div>
    {!pod ? <Blank title="Pod no longer observed">This Pod is not in the current snapshot. Return to the list; no other Pod is selected automatically.</Blank> : <>
      <Panel title={pod.id} id="pod-detail" action={<Phase state={pod.state} />}><Note>Pod · {view.application} · workload {pod.workload}</Note><Tabs activeKey={tab} onSelect={(_, key) => setTab(key)} aria-label="Pod detail tabs">{['details', 'logs', 'terminal', 'events', 'metrics'].map(key => <Tab key={key} eventKey={key} title={<TabTitleText>{key[0].toUpperCase() + key.slice(1)}</TabTitleText>} />)}</Tabs></Panel>
      <div key={`${pod.id}:${tab}`} id={`pod-${tab}`}>
        {tab === 'details' && <Panel title="Pod details"><Details items={[
          ['Name', pod.id], ['Application', view.application], ['Workload', pod.workload], ['Status', <Phase state={pod.state} />], ['Readiness', pod.draining ? 'Draining' : pod.ready ? 'Ready' : 'Not ready'], ['Pod IP', pod.ip || 'Unavailable'],
        ]} /><Note>Pod IPs are private to this application, not host endpoints. Node, restart count, creation time and Kubernetes owner references are not provided by this API.</Note>
          <DataTable label="Containers" columns={['Container', 'State', 'Exit', 'Readiness / liveness', 'Actions']} rows={list(pod.containers).map(c => [c.name, <Phase state={c.state} />, c.exitCode, `${c.ready ?? 'Unavailable'} / ${c.live ?? 'Unavailable'}`, <div className="gr-actions"><Button variant="link" isInline onClick={() => { setActiveContainer(c.name); setTab('logs'); }}>Logs</Button><Button variant="link" isInline isDisabled={c.state !== 'running'} onClick={() => { setActiveContainer(c.name); setTab('terminal'); }}>Terminal</Button><Button variant="link" isInline isDisabled={c.state !== 'running'} onClick={() => openExec(`${pod.id}/${c.name}`)}>Exec</Button></div>])} />
          <div className="gr-container-details">{list(workload?.containers).map(c => <div key={c.name}><Title headingLevel="h3" size="md">{c.name} {c.init && <Badge>Init</Badge>}</Title><Note>Image: {c.image}</Note><Note>Declared probes: startup {c.startupProbe ? 'yes' : 'no'} · readiness {c.readinessProbe ? 'yes' : 'no'} · liveness {c.livenessProbe ? 'yes' : 'no'}</Note>{list(c.mounts).map((m, i) => <p key={i}><code>{m.volume}</code> → <code>{m.path}</code> <Label isCompact>{m.readOnly ? 'Read-only' : 'Read-write'}</Label></p>)}</div>)}</div>
          <details className="gr-advanced"><summary>Runtime details (advanced)</summary><Details items={[
            ['Isolation', 'One hardware-isolated microVM per Pod'], ['Backend', pod.backend], ['Guest kernel version', 'Unavailable'], ['Allocation', `${pod.vcpu} vCPU · ${bytes(pod.guestBudgetBytes)} Pod environment memory budget`],
          ]} /></details>
        </Panel>}
        {tab === 'logs' && <Logs scopedResource={pod.id} preferredContainer={activeContainer} revision={gaps} />}
        {tab === 'terminal' && <PodTerminal view={view} pod={pod} preferredContainer={activeContainer} />}
        {tab === 'events' && <Events records={records} connection={connection} gaps={gaps} scopedResource={pod.id} />}
        {tab === 'metrics' && <Metrics view={{ ...view, sandboxes: [pod] }} />}
      </div>
    </>}
  </div>;
  return <Panel title="Pods" id="workloads"><Toolbar><ToolbarContent>
    <ToolbarItem><TextInput id="pod-search" maxLength={256} aria-label="Filter Pods by name" placeholder="Filter by name…" value={query} onChange={(_, value) => setQuery(value)} /></ToolbarItem>
    <ToolbarItem><FormSelect id="pod-state" aria-label="Filter Pods by status" value={state} onChange={(_, value) => setState(value)}><FormSelectOption value="" label="All statuses" />{[...new Set(list(view.sandboxes).map(s => s.state).filter(Boolean))].sort().map(s => <FormSelectOption key={s} value={s} label={s} />)}</FormSelect></ToolbarItem>
    <ToolbarItem><FormSelect id="pod-sort" aria-label="Sort Pods" value={sort} onChange={(_, value) => setSort(value)}><FormSelectOption value="name" label="Name" /><FormSelectOption value="state" label="Status" /><FormSelectOption value="workload" label="Workload" /></FormSelect></ToolbarItem>
    <ToolbarItem><Checkbox id="pod-descending" label="Descending" isChecked={descending} onChange={(_, value) => setDescending(value)} /></ToolbarItem><ToolbarItem><Button variant="secondary" onClick={() => { setQuery(''); setState(''); setSort('name'); setDescending(false); }}>Clear filters</Button></ToolbarItem>
  </ToolbarContent></Toolbar><div id="sandboxes"><DataTable label="Pods" columns={['Name', 'Status', 'Readiness', 'Workload', 'Pod IP', 'Actions']} rows={pods.map(p => [<Button variant="link" isInline onClick={() => inspect(p.id)}>{p.id}</Button>, <Phase state={p.state} />, <Label color={p.ready && !p.draining ? 'green' : 'grey'}>{p.draining ? 'Draining' : p.ready ? 'Ready' : 'Not ready'}</Label>, p.workload, p.ip || 'Unavailable', <div className="gr-actions"><Button variant="link" isInline onClick={() => inspect(p.id, 'logs')}>Logs</Button><Button variant="link" isInline onClick={() => inspect(p.id, 'terminal')}>Terminal</Button></div>])} /></div><Note>{pods.length} matching Pods · {list(view.sandboxes).length} observed. Statuses are reported by Grillo, not invented Kubernetes conditions.</Note></Panel>;
}
function Networking({ view, routes = false }) {
  return routes ? <Panel title="Routes" id="routes"><DataTable label="Routes" columns={['Hostname / path', 'Service', 'Published fallback']} rows={list(view.routes).map(r => [r.hostname + r.path, r.service, <RouteLink route={r} />])} /><Note>Grillo-published routes, not OpenShift Route API objects. Use the loopback fallback without editing /etc/hosts. UI and application endpoints are separate origins.</Note></Panel> : <Panel title="Services" id="services"><DataTable label="Services" columns={['Name', 'Discovery', 'TCP ports', 'Selected workloads (inferred)']} rows={list(view.services).map(s => [s.name, <Label color="blue">{s.headless ? 'Headless' : 'ClusterIP'}</Label>, list(s.ports).join(', '), list(s.workloads).join(', ') || 'None'])} /></Panel>;
}
function Storage({ view }) { return <Panel title="Volumes" id="volumes"><DataTable label="Volumes" columns={['Name', 'Kind', 'Access', 'Mode', 'Requested capacity']} rows={list(view.volumes).map(v => [v.name, <Label color={v.kind === 'bind' ? 'orange' : 'blue'}>{v.kind}</Label>, v.readOnly ? 'Read-only' : 'Read-write', v.accessMode || 'Not declared', bytes(v.capacity)])} /><Alert isInline variant="info" title="Bind mounts deliberately cross the sandbox boundary">Host source paths are not exposed here. Managed storage persists across down unless explicitly removed.</Alert></Panel>; }
function Configuration({ view, secrets = false }) {
  return <Panel title={secrets ? 'Secrets' : 'ConfigMaps'} id={secrets ? 'secrets' : 'configs'}>{secrets ? <DataTable label="Secrets" columns={['Name', 'Values']} rows={list(view.secrets).map(s => [s.name, <Label icon={<EyeSlashIcon />}>Hidden · no reveal endpoint</Label>])} /> : <DataTable label="ConfigMaps" columns={['Name', 'Entries', 'Contents']} rows={list(view.configs).map(c => [c.name, c.entryCount || 0, <Label icon={<EyeSlashIcon />}>Hidden</Label>])} />}<Note>Environment values, config contents, secret versions and rendered Helm values are not included in the public snapshot. No YAML editor or secret reveal is exposed.</Note></Panel>;
}
function Diagnostics({ view }) { return <div id="diagnostics" className="gr-stack"><Panel title="Source provenance"><Details items={[['Format', view.sourceKind], ['Source path', view.sourcePath]]} /></Panel>{list(view.diagnostics).map((d, i) => <Alert key={i} isInline variant={d.code.endsWith('_unavailable') ? 'info' : 'warning'} title={d.code}>{d.resource && <p><code>{d.resource}</code></p>}{d.message}</Alert>)}<Alert isInline variant="info" title="Unavailable does not mean zero">Guest/cgroup counters appear in Metrics when available. Guest kernel version, PSS, cache usage and CPU percentage are not provided by this API. Run <code>grillo plan &lt;source&gt;</code> for original field-level compiler diagnostics.</Alert></div>; }

function Logs({ revision, scopedResource = '', preferredContainer = '' }) {
  const [resource, setResource] = useState(scopedResource), [container, setContainer] = useState(preferredContainer), [stream, setStream] = useState('');
  const [follow, setFollow] = useState(false), [output, setOutput] = useState(''), [status, setStatus] = useState('Choose filters and read retained logs.');
  const state = useRef({ since: 0, text: '', generation: 0, controller: null });
  const read = useCallback(async (reset = false) => {
    if (state.current.controller && !reset) return;
    if (reset) { state.current.controller?.abort(); state.current.since = 0; state.current.text = ''; state.current.generation++; setOutput(''); }
    const generation = state.current.generation, controller = new AbortController();
    state.current.controller = controller;
    try {
      const query = new URLSearchParams({ since: String(state.current.since), resource, container });
      const data = await request('/v1/logs?' + query, { signal: controller.signal });
      if (generation !== state.current.generation) return;
      const next = appendLogs(state.current.text, state.current.since, data.records, stream);
      state.current.since = next.since; state.current.text = next.text; setOutput(next.text);
      setStatus(next.count ? `Read ${next.count} records. Display retains at most 256 KiB.` : 'No new matching retained records. Check diagnostics for guest log collection failures.');
    } catch (e) { if (e.name !== 'AbortError') setStatus(e.message); }
    finally { if (state.current.controller === controller) state.current.controller = null; }
  }, [resource, container, stream]);
  useEffect(() => { read(true); return () => state.current.controller?.abort(); }, [read, revision]);
  useEffect(() => { if (!follow) return; const timer = setInterval(() => read(), 2000); return () => clearInterval(timer); }, [follow, read]);
  return <Panel title={scopedResource ? `Pod logs · ${scopedResource}` : 'Container logs'}><Toolbar><ToolbarContent><ToolbarItem><TextInput id="log-resource" isDisabled={!!scopedResource} aria-label="Exact resource" placeholder="Exact resource (all if empty)" value={resource} onChange={(_, v) => setResource(v)} /></ToolbarItem><ToolbarItem><TextInput id="log-container" aria-label="Log container" placeholder="Container" value={container} onChange={(_, v) => setContainer(v)} /></ToolbarItem><ToolbarItem><FormSelect id="log-stream" aria-label="Log stream" value={stream} onChange={(_, v) => setStream(v)}><FormSelectOption value="" label="Both streams" /><FormSelectOption value="stdout" label="stdout" /><FormSelectOption value="stderr" label="stderr" /></FormSelect></ToolbarItem><ToolbarItem><Button id="logs-refresh" variant="secondary" icon={<SyncAltIcon />} onClick={() => read(true)}>Read logs</Button></ToolbarItem><ToolbarItem><Checkbox id="logs-follow" label="Follow" isChecked={follow} onChange={(_, v) => setFollow(v)} /></ToolbarItem></ToolbarContent></Toolbar><Note><span id="logs-status" role="status">{status}</span></Note><pre id="logs" className="gr-output gr-log-output">{output || ''}</pre><Note>Log output is plain text, never HTML. Empty retained results do not prove silence or health. Guest retention gaps remain visible even with stream filters; output deliberately printed by a workload is not secret-redacted.</Note></Panel>;
}
function Events({ records, connection, gaps, scopedResource = '' }) {
  const [filter, setFilter] = useState('');
  const scoped = scopedResource ? podEvents(records, scopedResource) : records;
  return <Panel title={scopedResource ? `Pod events · ${scopedResource}` : 'Daemon events'}><Toolbar><ToolbarContent><ToolbarItem><TextInput id="event-filter" aria-label="Event resource prefix" value={filter} placeholder="Filter by resource prefix" onChange={(_, v) => setFilter(v)} /></ToolbarItem><ToolbarItem><Label color={connection === 'Connected' ? 'green' : 'grey'}><span id="events-status">{connection}</span></Label></ToolbarItem><ToolbarItem><Badge>{scoped.length} retained in view</Badge></ToolbarItem></ToolbarContent></Toolbar>{gaps > 0 && <Alert isInline variant="info" title={`${gaps} retention gap(s) detected`}>Application snapshots and log cursors were refreshed. Missing retained history is not reconstructed.</Alert>}<pre id="events" className="gr-output">{scoped.filter(e => !filter || (e.resource || '').startsWith(filter)).map(e => JSON.stringify(e)).join('\n') || (scopedResource ? 'No matching retained Pod events.' : '')}</pre><Note>{scopedResource ? 'Only exact Pod and Pod/container resource IDs from the retained daemon stream. Application/controller events are not attributed to this Pod. Empty results do not prove health.' : 'Global daemon events, not measured workload traffic.'} The stream retains at most 200 events / 256 KiB in this browser, suppresses repeated IDs and resumes via Last-Event-ID.</Note></Panel>;
}
function Exec({ view, preferredTarget }) {
  const targets = running(view), [target, setTarget] = useState(preferredTarget || targets[0]?.target || '');
  const [args, setArgs] = useState('["/bin/busybox", "id"]'), [status, setStatus] = useState('Ready to run a command.');
  const [stdout, setStdout] = useState(''), [stderr, setStderr] = useState(''), [busy, setBusy] = useState(false);
  const controller = useRef(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function run() {
    let command;
    try { command = parseCommand(args); if (!targets.some(t => t.target === target)) throw new Error('Select a running container.'); } catch (e) { setStatus(e.message); return; }
    controller.current = new AbortController(); setBusy(true); setStdout(''); setStderr(''); setStatus('Running…');
    try {
      const data = await request('/v1/exec', { method: 'POST', headers: { 'Content-Type': 'application/json' }, signal: controller.current.signal, body: JSON.stringify({ application: view.application, container: target, args: command }) });
      setStdout(data.stdout); setStderr(data.stderr); setStatus(`Exit code ${data.exitCode}${data.truncated ? ' (output truncated)' : ''}`);
    } catch (e) { setStatus(e.name === 'AbortError' ? 'Request canceled.' : e.message); }
    finally { controller.current = null; setBusy(false); }
  }
  return <div className="gr-stack"><Panel title="Execute in a container"><Form><FormGroup label="Target container" fieldId="exec-target"><FormSelect id="exec-target" value={target} onChange={(_, v) => setTarget(v)} isDisabled={busy} aria-label="Exec target">{targets.length ? targets.map(c => <FormSelectOption key={c.target} value={c.target} label={c.target} />) : <FormSelectOption value="" label="No running containers" />}</FormSelect></FormGroup><FormGroup label="Command (JSON argument array)" fieldId="exec-args"><TextArea id="exec-args" value={args} onChange={(_, v) => setArgs(v)} rows={3} isDisabled={busy} /></FormGroup><div className="gr-actions"><Button id="exec-run" onClick={run} isDisabled={busy || !targets.length} icon={<TerminalIcon />}>Run in container</Button><Button id="exec-cancel" variant="secondary" isDisabled={!busy} onClick={() => controller.current?.abort()}>Cancel request</Button><Label><span id="exec-status" role="status">{status}</span></Label></div></Form><Note>Non-TTY, captured on completion. 30-second deadline · 1 MiB total output limit. No host shell expansion. Output deliberately printed by a workload is not secret-redacted.</Note></Panel><Grid hasGutter><GridItem span={12} lg={6}><Panel title="stdout"><pre id="exec-stdout" className="gr-output">{stdout}</pre></Panel></GridItem><GridItem span={12} lg={6}><Panel title="stderr"><pre id="exec-stderr" className="gr-output">{stderr}</pre></Panel></GridItem></Grid><Alert isInline variant="info" title="Need an interactive shell?">Open Workloads → Pods → Terminal for an interactive browser shell, or use <code id="shell-guide">grillo shell &lt;application&gt; &lt;Pod-ID/container&gt;</code>. Distroless images may not contain a shell.</Alert></div>;
}

function Console() {
  const [applications, setApplications] = useState([]), [application, setApplication] = useState(''), [view, setView] = useState(null);
  const [section, setSection] = useState('overview'), [sidebarOpen, setSidebarOpen] = useState(window.innerWidth >= MOBILE_WIDTH);
  const [loading, setLoading] = useState(true), [error, setError] = useState(''), [authorized, setAuthorized] = useState(false);
  const [records, setRecords] = useState([]), [connection, setConnection] = useState('Connecting…'), [gaps, setGaps] = useState(0);
  const [preferredTarget, setPreferredTarget] = useState(''), [podConsole, setPodConsole] = useState({ pod: '', mode: '' });
  const selected = useRef(''), snapshotRequest = useRef(null), sequence = useRef(0), retained = useRef([]), refreshRef = useRef(null);
  const wasMobile = useRef(window.innerWidth < MOBILE_WIDTH);
  function onPageResize(_, { mobileView }) {
    if (wasMobile.current !== mobileView) { wasMobile.current = mobileView; setSidebarOpen(!mobileView); }
  }
  const loadView = useCallback(async name => {
    snapshotRequest.current?.abort();
    const controller = new AbortController(); snapshotRequest.current = controller;
    if (!name) { setView(null); setLoading(false); return; }
    setLoading(true); setError('');
    try { const data = await request('/v1/applications/' + encodeURIComponent(name) + '/view', { signal: controller.signal }); if (!controller.signal.aborted) setView(data); }
    catch (e) { if (e.name !== 'AbortError') { setError(e.message); setView(null); } }
    finally { if (!controller.signal.aborted) setLoading(false); }
  }, []);
  const refresh = useCallback(async () => {
    try {
      const data = await request('/v1/applications'); const names = list(data.applications);
      const name = names.includes(selected.current) ? selected.current : names[0] || '';
      setApplications(names); selected.current = name; setApplication(name); await loadView(name);
    } catch (e) { setError(e.message); setLoading(false); }
  }, [loadView]);
  refreshRef.current = refresh;
  useEffect(() => {
    let disposed = false;
    startup.then(error => { if (!disposed) { if (error) { setError(error.message); setLoading(false); } else { setAuthorized(true); refresh(); } } });
    return () => { disposed = true; snapshotRequest.current?.abort(); };
  }, [refresh]);
  useEffect(() => {
    if (!authorized) return;
    const source = new EventSource('/v1/events');
    source.onopen = () => setConnection('Connected');
    source.onerror = () => setConnection('Reconnecting…');
    source.onmessage = event => {
      try {
        const data = JSON.parse(event.data), next = appendEvent(retained.current, data, sequence.current);
        if (!next.added) return;
        retained.current = next.records; sequence.current = next.lastSequence; setRecords(next.records);
        if (next.dropped) setConnection('Oversized event omitted');
        if (data.kind === 'events.gap') { setGaps(n => n + 1); refreshRef.current(); }
      } catch { setConnection('Malformed event ignored'); }
    };
    const close = () => source.close(); window.addEventListener('pagehide', close);
    return () => { close(); window.removeEventListener('pagehide', close); };
  }, [authorized]);
  function choose(name) { selected.current = name; setApplication(name); setPreferredTarget(''); setPodConsole({ pod: '', mode: '' }); setView(null); loadView(name); }
  function navigate(key, preservePod = false) { if (key === 'workloads' && !preservePod) setPodConsole({ pod: '', mode: '' }); setSection(key); if (window.innerWidth < MOBILE_WIDTH) setSidebarOpen(false); }
  function openExec(target) { setPreferredTarget(target); navigate('exec'); }
  function openPodConsole(pod, mode) { setPodConsole({ pod, mode }); navigate('workloads', true); }
  const heading = sections.find(([key]) => key === section)[1], group = navigation.find(([, pages]) => pages.some(([key]) => key === section))[0];
  const masthead = <Masthead className="gr-masthead" display={{ default: 'inline' }}><MastheadMain><MastheadToggle><PageToggleButton variant="plain" aria-label="Toggle navigation" isSidebarOpen={sidebarOpen} onSidebarToggle={() => setSidebarOpen(v => !v)}><BarsIcon /></PageToggleButton></MastheadToggle><MastheadBrand><MastheadLogo component="div"><img className="gr-brand-mark" src="/assets/generated/icons/g-foglia-32.png" srcSet="/assets/generated/icons/g-foglia-32.png 1x, /assets/generated/icons/g-foglia-64.png 2x" width="32" height="32" alt="" /><span className="gr-brand">grillo</span></MastheadLogo></MastheadBrand></MastheadMain><MastheadContent><span className="gr-masthead-caption">Application console</span><span className="gr-header-context"><CubesIcon /> Local workspace</span><ThemePicker /><Label variant="outline" className="gr-release">Experimental</Label></MastheadContent></Masthead>;
  const sidebar = <PageSidebar isSidebarOpen={sidebarOpen} inert={!sidebarOpen}><PageSidebarBody><div className="gr-sidebar-context"><CubesIcon /><div><strong>Local workspace</strong><span>{application || 'Select an application'}</span></div></div><Nav aria-label="Console navigation">{navigation.map(([title, pages]) => <NavGroup key={title} title={title}><NavList>{pages.map(([key, label, Icon]) => <NavItem key={key} itemId={key} isActive={section === key} component="button" onClick={() => navigate(key)} icon={<Icon />} data-testid={'nav-' + key}>{label}</NavItem>)}</NavList></NavGroup>)}</Nav><div className="gr-sidebar-footer"><p>Closing the console leaves workloads running.</p><p><a href="/assets/generated/licenses.txt" target="_blank" rel="noopener noreferrer">Third-party notices</a></p></div></PageSidebarBody></PageSidebar>;
  let content = null;
  if (view) {
    const pages = {
      overview: <Overview view={view} navigate={navigate} events={records} />, workloads: <Workloads key={`${application}:${podConsole.pod}:${podConsole.mode}`} view={view} openExec={openExec} records={records} connection={connection} gaps={gaps} preferredPod={podConsole.pod} preferredMode={podConsole.mode} />,
      controllers: <Controllers view={view} />, routes: <Networking view={view} routes />, secrets: <Configuration view={view} secrets />, metrics: <Metrics view={view} />,
      topology: <Topology key={application} view={view} openLogs={pod => openPodConsole(pod, 'logs')} openTTY={pod => openPodConsole(pod, 'tty')} />, networking: <Networking view={view} />, storage: <Storage view={view} />,
      configuration: <Configuration view={view} />, logs: <Logs revision={gaps} />, events: <Events records={records} connection={connection} gaps={gaps} />,
      exec: <Exec key={application} view={view} preferredTarget={preferredTarget} />, diagnostics: <Diagnostics view={view} />,
    };
    content = pages[section];
  }
  return <Page masthead={masthead} sidebar={sidebar} onPageResize={onPageResize} mainContainerId="main-content" className="gr-page" data-console="patternfly-react"><PageSection className="gr-page-heading"><div className="gr-page-eyebrow">Workspace / {application || 'Applications'} / {group} / {heading}</div><div className="gr-title-row"><div><Title headingLevel="h1" size="2xl">{heading}</Title><p className="gr-subtitle">{help[section]}</p></div><Label color="teal">{application || 'No application'}</Label></div><Toolbar className="gr-context-toolbar"><ToolbarContent><ToolbarItem><span className="gr-context-label">Application</span></ToolbarItem><ToolbarItem><FormSelect id="apps" aria-label="Application" value={application} onChange={(_, name) => choose(name)} isDisabled={!authorized || !applications.length}>{applications.length ? applications.map(name => <FormSelectOption key={name} value={name} label={name} />) : <FormSelectOption value="" label="No applications" />}</FormSelect></ToolbarItem><ToolbarItem><Button id="refresh" variant="secondary" icon={<SyncAltIcon />} onClick={refresh} isDisabled={!authorized || loading}>Refresh</Button></ToolbarItem><ToolbarItem><span id="status" className="gr-muted" role="status">{loading ? (view ? 'Refreshing snapshot…' : 'Loading snapshot…') : `${applications.length} application(s)`}</span></ToolbarItem></ToolbarContent></Toolbar></PageSection><PageSection variant="secondary" isFilled>{error && <Alert isInline variant="danger" title="Console unavailable">{error}</Alert>}{loading && !view ? <div className="gr-loading"><Spinner size="xl" aria-label="Loading application" /></div> : content || <Blank title="No application selected">Start a supported application with <code>grillo up</code>, then refresh. No workload is provisioned by opening this console.</Blank>}</PageSection></Page>;
}
class Boundary extends React.Component {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <main className="gr-fatal"><Alert isInline variant="danger" title="Unable to render the console">Refresh the page and check daemon compatibility. Workloads are not stopped.</Alert></main> : this.props.children; }
}
createRoot(document.getElementById('root')).render(<React.StrictMode><Boundary><Console /></Boundary></React.StrictMode>);
