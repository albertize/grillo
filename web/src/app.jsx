// SPDX-License-Identifier: Apache-2.0
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Alert, Badge, Button, Card, CardBody, CardHeader, CardTitle, Checkbox,
  DescriptionList, DescriptionListDescription, DescriptionListGroup, DescriptionListTerm,
  EmptyState, EmptyStateBody, Form, FormGroup, FormSelect, FormSelectOption,
  Grid, GridItem, Label, Masthead, MastheadBrand, MastheadContent, MastheadLogo,
  MastheadMain, MastheadToggle, Nav, NavItem, NavList, Page, PageSection,
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
import { appendEvent, appendLogs, bootstrapSession, bytes, parseCommand, request, safeRoute } from './client.mjs';

// Outside React effects: StrictMode must never exchange a monouse token twice.
const startup = bootstrapSession().then(() => null, error => error);
const sections = [
  ['overview', 'Overview', ThLargeIcon], ['workloads', 'Workloads', CubesIcon],
  ['topology', 'Topology', ProjectDiagramIcon], ['networking', 'Networking', NetworkWiredIcon],
  ['storage', 'Storage', DatabaseIcon], ['configuration', 'Configuration', CogIcon],
  ['logs', 'Logs', StreamIcon], ['events', 'Events', StreamIcon],
  ['exec', 'Exec', TerminalIcon], ['diagnostics', 'Diagnostics', ExclamationCircleIcon],
];
// PatternFly 6.6.1's XL breakpoint is 75rem (1200px).
const MOBILE_WIDTH = 1200;
const help = {
  overview: 'Your application at a glance. Desired state and real microVM observations, in one place.',
  workloads: 'Inspect workload replicas, containers and their hardware-isolation boundaries.',
  topology: 'Declared routes and Service selectors. Relationships are inferred, not measured traffic.',
  networking: 'Service discovery and published application endpoints. Management stays separate.',
  storage: 'Persistent, ephemeral and explicitly shared storage attached to this application.',
  configuration: 'Configuration and Secret metadata only. Values remain outside this public view.',
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
  return <Panel title="Resource accounting" id="metrics"><DataTable label="VMM resource samples" columns={['MicroVM', 'Allocated vCPU', 'Guest budget', 'VMM RSS', 'CPU time (/proc)']} rows={list(view.sandboxes).map(s => [s.id, s.vcpu, bytes(s.guestBudgetBytes), bytes(s.vmm?.rssBytes), s.vmm ? `${s.vmm.cpuTimeMs} ms · ${new Date(s.vmm.time).toLocaleTimeString()}` : 'Unavailable'])} /><DataTable label="Guest resource samples" columns={['MicroVM', 'Guest total', 'Guest available', 'Busy / idle USER_HZ ticks', 'Collected / source']} rows={list(view.sandboxes).map(s => [s.id, bytes(s.guest?.memoryTotalBytes), bytes(s.guest?.memoryAvailableBytes), s.guest ? `${s.guest.cpuBusyTicks ?? 'Unavailable'} / ${s.guest.cpuIdleTicks ?? 'Unavailable'} ticks` : 'Unavailable', s.guest ? `${new Date(s.guest.time).toLocaleTimeString()} · ${s.guest.source}` : 'Unavailable'])} /><DataTable label="Container cgroup samples" columns={['MicroVM / container', 'Memory (cgroup)', 'CPU usage (cumulative)', 'Collected / source']} rows={list(view.sandboxes).flatMap(s => list(s.containers).map(c => [`${s.id}/${c.name}`, bytes(c.usage?.memoryBytes), c.usage?.cpuUsec !== undefined ? `${c.usage.cpuUsec} µs` : 'Unavailable', c.usage ? `${new Date(c.usage.time).toLocaleTimeString()} · ${c.usage.source}` : 'Unavailable']))} /><Note>Guest budget is allocation, not usage. VMM RSS, guest/container actual usage and cache are different accounting domains; they are not summed. PSS and CPU percentage remain unavailable. Guest counters and container cgroup usage are independently sampled, not summed; absent values are unavailable. Guest-reported data is not host attestation.</Note></Panel>;
}
function Overview({ view, navigate, events }) {
  const vms = list(view.sandboxes), ready = vms.filter(s => s.ready && !s.draining).length;
  const desired = list(view.workloads).reduce((n, w) => n + w.replicas, 0);
  const stats = [
    ['MicroVMs ready', `${ready} / ${vms.length}`, `${desired} desired replicas`, ServerIcon, 'workloads'],
    ['Running containers', running(view).length, 'Init containers are listed separately', CubesIcon, 'workloads'],
    ['Services', list(view.services).length, 'Private application discovery', NetworkWiredIcon, 'networking'],
    ['Routes', list(view.routes).length, 'Loopback fallback endpoints', ExternalLinkAltIcon, 'networking'],
  ];
  return <div className="gr-stack" id="overview">
    <Grid hasGutter>{stats.map(([name, value, sub, Icon, section]) => <GridItem key={name} span={12} sm={6} lg={3}><Card className="gr-stat"><CardBody><div className="gr-stat-heading"><Icon /><span>{name}</span></div><div className="gr-stat-value">{value}</div><Note>{sub}</Note><Button variant="link" isInline onClick={() => navigate(section)}>View details</Button></CardBody></Card></GridItem>)}</Grid>
    <Panel title={view.application + ' · application status'}><div className="gr-status-line"><Label color={vms.length && ready === vms.length ? 'green' : 'grey'} icon={<CheckCircleIcon />}>{vms.length ? `${ready}/${vms.length} observed microVMs ready` : 'No microVMs observed'}</Label><Label color="blue">One Pod · one microVM</Label><Label variant="outline">{view.sourceKind || 'Native'}</Label></div><Details items={[
      ['Application', view.application], ['Source', <span id="source">{view.sourceKind || 'Unavailable'} · {view.sourcePath || 'Source path unavailable'}</span>], ['Snapshot', new Date(view.time).toLocaleString()],
    ]} /></Panel>
    <Grid hasGutter><GridItem span={12} lg={7}><Panel title="Published routes" id="routes" action={<Button variant="link" onClick={() => navigate('networking')}>Networking</Button>}>{list(view.routes).length ? list(view.routes).map((r, i) => <div className="gr-route" key={i}><strong>{r.hostname}{r.path}</strong><Note>Service {r.service}</Note><RouteLink route={r} /></div>) : <Note>No published routes.</Note>}</Panel></GridItem><GridItem span={12} lg={5}><Panel title="Recent activity" action={<Button variant="link" onClick={() => navigate('events')}>All events</Button>}>{events.length ? events.slice(-4).reverse().map(e => <div className="gr-activity" key={e.seq}><Label color="blue" isCompact>{e.kind}</Label><Note>{e.resource || 'Daemon'} · {e.message || ''}</Note></div>) : <Note>No retained daemon events. Sparse activity does not imply a healthy or silent workload.</Note>}</Panel></GridItem></Grid>
    <Metrics view={view} />
  </div>;
}
function Workloads({ view, openExec }) {
  const [selected, setSelected] = useState('');
  const vms = list(view.sandboxes), vm = vms.find(s => s.id === selected) || vms[0];
  const workload = list(view.workloads).find(w => w.id === vm?.workload);
  return <div className="gr-stack">
    <Panel title="Workloads" id="workloads"><DataTable label="Workloads" columns={['Name', 'Kind', 'Desired replicas', 'Containers']} rows={list(view.workloads).map(w => [w.id, <Label color="blue">{w.kind}</Label>, w.replicas, list(w.containers).map(c => <Label key={c.name} color={c.init ? 'grey' : 'teal'}>{c.name}{c.init ? ' · init' : ''}</Label>)])} /></Panel>
    <Panel title="MicroVM sandboxes" id="sandboxes"><DataTable label="Sandboxes" columns={['MicroVM', 'Workload', 'State', 'Readiness', 'Private IP']} rows={vms.map(s => [<Button variant="link" isInline onClick={() => setSelected(s.id)}>{s.id}</Button>, s.workload, <Phase state={s.state} />, <Label color={s.ready && !s.draining ? 'green' : 'grey'}>{s.draining ? 'Draining' : s.ready ? 'Ready' : 'Not ready'}</Label>, s.ip || 'Unavailable'])} /></Panel>
    {vm && <Panel title={'Sandbox detail · ' + vm.id}>
      <Details items={[
        ['Boundary', 'One hardware-isolated microVM'], ['Backend', vm.backend], ['Workload', vm.workload], ['Private IP', vm.ip || 'Unavailable'], ['Guest kernel version', 'Unavailable'], ['Allocation', `${vm.vcpu} vCPU · ${bytes(vm.guestBudgetBytes)} guest budget`],
      ]} /><Note>Pod addresses are private to the application namespace, not host endpoints.</Note>
      <DataTable label="Containers" columns={['Container', 'State', 'Exit', 'Readiness / liveness', 'Actions']} rows={list(vm.containers).map(c => [c.name, <Phase state={c.state} />, c.exitCode, `${c.ready ?? 'Unavailable'} / ${c.live ?? 'Unavailable'}`, <Button variant="link" isInline isDisabled={c.state !== 'running'} onClick={() => openExec(`${vm.id}/${c.name}`)}>Exec</Button>])} />
      <div className="gr-container-details">{list(workload?.containers).map(c => <div key={c.name}><Title headingLevel="h3" size="md">{c.name} {c.init && <Badge>Init</Badge>}</Title><Note>Image: {c.image}</Note><Note>Declared probes: startup {c.startupProbe ? 'yes' : 'no'} · readiness {c.readinessProbe ? 'yes' : 'no'} · liveness {c.livenessProbe ? 'yes' : 'no'}</Note>{list(c.mounts).map((m, i) => <p key={i}><code>{m.volume}</code> → <code>{m.path}</code> <Label isCompact>{m.readOnly ? 'Read-only' : 'Read-write'}</Label></p>)}</div>)}</div>
    </Panel>}
  </div>;
}
function Topology({ view }) {
  const vms = list(view.sandboxes).slice(0, 100), services = list(view.services).slice(0, 100), routes = list(view.routes).slice(0, 100);
  const height = Math.max(260, Math.max(vms.length, services.length, routes.length) * 100 + 70);
  const y = i => 70 + i * 100;
  return <Panel title="Application topology"><div className="gr-topology"><svg id="topology" viewBox={`0 0 960 ${height}`} role="img" aria-label="Routes to Services to isolated microVMs">
    {routes.map((r, i) => { const target = services.findIndex(s => s.name === r.service); return target >= 0 && <path key={'route-edge-' + i} className="gr-graph-edge" d={`M270 ${y(i)} C310 ${y(i)},310 ${y(target)},350 ${y(target)}`} />; })}
    {services.flatMap((s, i) => vms.map((vm, j) => list(s.workloads).includes(vm.workload) && <path key={`${i}-${j}`} className="gr-graph-edge" d={`M610 ${y(i)} C650 ${y(i)},650 ${y(j)},690 ${y(j)}`} />))}
    {[['Routes', routes, 10, (r) => `${r.hostname}${r.path}`, () => 'Loopback endpoint'], ['Services', services, 350, s => s.name, s => s.headless ? 'Headless discovery' : 'TCP Service'], ['MicroVM boundaries', vms, 690, s => s.id, s => `${s.backend} · ${s.ready ? 'Ready' : 'Not ready'}`]].map(([heading, nodes, x, name, detail]) => <g key={heading}><text x={x} y="18" className="gr-graph-heading">{heading}</text>{nodes.map((n, i) => <g key={i}><rect x={x} y={y(i) - 32} width="260" height="64" rx="10" /><text x={x + 14} y={y(i) - 4} className="gr-graph-name"><title>{name(n)}</title>{name(n).slice(0, 32)}</text><text x={x + 14} y={y(i) + 17} className="gr-graph-detail">{detail(n).slice(0, 38)}</text></g>)}</g>)}
  </svg></div><Note>Edges are inferred from declared route backends and Service selectors, not traffic telemetry. Diagram is capped at 100 nodes per kind.</Note></Panel>;
}
function Networking({ view }) { return <div className="gr-stack"><Panel title="Services"><DataTable label="Services" columns={['Name', 'Discovery', 'TCP ports', 'Selected workloads (inferred)']} rows={list(view.services).map(s => [s.name, <Label color="blue">{s.headless ? 'Headless' : 'ClusterIP'}</Label>, list(s.ports).join(', '), list(s.workloads).join(', ') || 'None'])} /></Panel><Panel title="Routes" id="routes"><DataTable label="Routes" columns={['Hostname / path', 'Service', 'Published fallback']} rows={list(view.routes).map(r => [r.hostname + r.path, r.service, <RouteLink route={r} />])} /><Note>Use the loopback fallback without editing /etc/hosts. UI and application endpoints are separate origins.</Note></Panel></div>; }
function Storage({ view }) { return <Panel title="Volumes" id="volumes"><DataTable label="Volumes" columns={['Name', 'Kind', 'Access', 'Mode', 'Requested capacity']} rows={list(view.volumes).map(v => [v.name, <Label color={v.kind === 'bind' ? 'orange' : 'blue'}>{v.kind}</Label>, v.readOnly ? 'Read-only' : 'Read-write', v.accessMode || 'Not declared', bytes(v.capacity)])} /><Alert isInline variant="info" title="Bind mounts deliberately cross the sandbox boundary">Host source paths are not exposed here. Managed storage persists across down unless explicitly removed.</Alert></Panel>; }
function Configuration({ view }) {
  const [tab, setTab] = useState('configs');
  return <Panel title="Configuration metadata"><Tabs activeKey={tab} onSelect={(_, key) => setTab(key)} aria-label="Configuration tabs"><Tab eventKey="configs" title={<TabTitleText>ConfigMaps <Badge>{list(view.configs).length}</Badge></TabTitleText>}><div id="configs" className="gr-tab-content"><DataTable label="Configs" columns={['Name', 'Entries', 'Contents']} rows={list(view.configs).map(c => [c.name, c.entryCount || 0, <Label icon={<EyeSlashIcon />}>Hidden</Label>])} /></div></Tab><Tab eventKey="secrets" title={<TabTitleText>Secrets <Badge>{list(view.secrets).length}</Badge></TabTitleText>}><div id="secrets" className="gr-tab-content"><DataTable label="Secrets" columns={['Name', 'Values']} rows={list(view.secrets).map(s => [s.name, <Label icon={<EyeSlashIcon />}>Hidden · no reveal endpoint</Label>])} /></div></Tab></Tabs><Note>Environment values, config contents, secret versions and rendered Helm values are not included in the public snapshot.</Note></Panel>;
}
function Diagnostics({ view }) { return <div id="diagnostics" className="gr-stack"><Panel title="Source provenance"><Details items={[['Format', view.sourceKind], ['Source path', view.sourcePath]]} /></Panel>{list(view.diagnostics).map((d, i) => <Alert key={i} isInline variant={d.code.endsWith('_unavailable') ? 'info' : 'warning'} title={d.code}>{d.resource && <p><code>{d.resource}</code></p>}{d.message}</Alert>)}<Alert isInline variant="info" title="Unavailable does not mean zero">Guest/cgroup counters appear in Metrics when available. Guest kernel version, PSS, cache usage and CPU percentage are not provided by this API. Run <code>grillo plan &lt;source&gt;</code> for original field-level compiler diagnostics.</Alert></div>; }

function Logs({ revision }) {
  const [resource, setResource] = useState(''), [container, setContainer] = useState(''), [stream, setStream] = useState('');
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
  return <Panel title="Container logs"><Toolbar><ToolbarContent><ToolbarItem><TextInput id="log-resource" aria-label="Exact resource" placeholder="Exact resource (all if empty)" value={resource} onChange={(_, v) => setResource(v)} /></ToolbarItem><ToolbarItem><TextInput id="log-container" aria-label="Log container" placeholder="Container" value={container} onChange={(_, v) => setContainer(v)} /></ToolbarItem><ToolbarItem><FormSelect id="log-stream" aria-label="Log stream" value={stream} onChange={(_, v) => setStream(v)}><FormSelectOption value="" label="Both streams" /><FormSelectOption value="stdout" label="stdout" /><FormSelectOption value="stderr" label="stderr" /></FormSelect></ToolbarItem><ToolbarItem><Button id="logs-refresh" variant="secondary" icon={<SyncAltIcon />} onClick={() => read(true)}>Read logs</Button></ToolbarItem><ToolbarItem><Checkbox id="logs-follow" label="Follow" isChecked={follow} onChange={(_, v) => setFollow(v)} /></ToolbarItem></ToolbarContent></Toolbar><Note><span id="logs-status" role="status">{status}</span></Note><pre id="logs" className="gr-output gr-log-output">{output || ''}</pre><Note>Log output is plain text, never HTML. Empty retained results do not prove silence or health. Guest retention gaps remain visible even with stream filters; output deliberately printed by a workload is not secret-redacted.</Note></Panel>;
}
function Events({ records, connection, gaps }) {
  const [filter, setFilter] = useState('');
  return <Panel title="Daemon events"><Toolbar><ToolbarContent><ToolbarItem><TextInput id="event-filter" aria-label="Event resource prefix" value={filter} placeholder="Filter by resource prefix" onChange={(_, v) => setFilter(v)} /></ToolbarItem><ToolbarItem><Label color={connection === 'Connected' ? 'green' : 'grey'}><span id="events-status">{connection}</span></Label></ToolbarItem><ToolbarItem><Badge>{records.length} retained in view</Badge></ToolbarItem></ToolbarContent></Toolbar>{gaps > 0 && <Alert isInline variant="info" title={`${gaps} retention gap(s) detected`}>Application snapshots and log cursors were refreshed. Missing retained history is not reconstructed.</Alert>}<pre id="events" className="gr-output">{records.filter(e => !filter || (e.resource || '').startsWith(filter)).map(e => JSON.stringify(e)).join('\n')}</pre><Note>Global daemon events, not measured workload traffic. The view stores at most 200 events / 256 KiB, suppresses repeated IDs and resumes via Last-Event-ID.</Note></Panel>;
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
  return <div className="gr-stack"><Panel title="Execute in a container"><Form><FormGroup label="Target container" fieldId="exec-target"><FormSelect id="exec-target" value={target} onChange={(_, v) => setTarget(v)} isDisabled={busy} aria-label="Exec target">{targets.length ? targets.map(c => <FormSelectOption key={c.target} value={c.target} label={c.target} />) : <FormSelectOption value="" label="No running containers" />}</FormSelect></FormGroup><FormGroup label="Command (JSON argument array)" fieldId="exec-args"><TextArea id="exec-args" value={args} onChange={(_, v) => setArgs(v)} rows={3} isDisabled={busy} /></FormGroup><div className="gr-actions"><Button id="exec-run" onClick={run} isDisabled={busy || !targets.length} icon={<TerminalIcon />}>Run in container</Button><Button id="exec-cancel" variant="secondary" isDisabled={!busy} onClick={() => controller.current?.abort()}>Cancel request</Button><Label><span id="exec-status" role="status">{status}</span></Label></div></Form><Note>Non-TTY, captured on completion. 30-second deadline · 1 MiB total output limit. No host shell expansion. Output deliberately printed by a workload is not secret-redacted.</Note></Panel><Grid hasGutter><GridItem span={12} lg={6}><Panel title="stdout"><pre id="exec-stdout" className="gr-output">{stdout}</pre></Panel></GridItem><GridItem span={12} lg={6}><Panel title="stderr"><pre id="exec-stderr" className="gr-output">{stderr}</pre></Panel></GridItem></Grid><Alert isInline variant="info" title="Need an interactive shell?">Full browser TTY/stdin/resize is not implemented. Use <code id="shell-guide">grillo shell &lt;application&gt; &lt;sandbox-ID/container&gt;</code>. Distroless images may not contain a shell.</Alert></div>;
}

function Console() {
  const [applications, setApplications] = useState([]), [application, setApplication] = useState(''), [view, setView] = useState(null);
  const [section, setSection] = useState('overview'), [sidebarOpen, setSidebarOpen] = useState(window.innerWidth >= MOBILE_WIDTH);
  const [loading, setLoading] = useState(true), [error, setError] = useState(''), [authorized, setAuthorized] = useState(false);
  const [records, setRecords] = useState([]), [connection, setConnection] = useState('Connecting…'), [gaps, setGaps] = useState(0);
  const [preferredTarget, setPreferredTarget] = useState('');
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
  function choose(name) { selected.current = name; setApplication(name); setPreferredTarget(''); setView(null); loadView(name); }
  function navigate(key) { setSection(key); if (window.innerWidth < MOBILE_WIDTH) setSidebarOpen(false); }
  function openExec(target) { setPreferredTarget(target); navigate('exec'); }
  const heading = sections.find(([key]) => key === section)[1];
  const masthead = <Masthead className="gr-masthead"><MastheadMain><MastheadToggle><PageToggleButton variant="plain" aria-label="Toggle navigation" isSidebarOpen={sidebarOpen} onSidebarToggle={() => setSidebarOpen(v => !v)}><BarsIcon /></PageToggleButton></MastheadToggle><MastheadBrand><MastheadLogo component="div"><span className="gr-brand-mark">g</span><span className="gr-brand">grillo</span></MastheadLogo></MastheadBrand></MastheadMain><MastheadContent><span className="gr-masthead-caption">Local application runtime</span><Label color="teal">Linux / KVM</Label><Label variant="outline" className="gr-release">Pre-release</Label></MastheadContent></Masthead>;
  const sidebar = <PageSidebar isSidebarOpen={sidebarOpen} inert={!sidebarOpen}><PageSidebarBody><Nav aria-label="Console navigation"><NavList>{sections.map(([key, label, Icon]) => <NavItem key={key} itemId={key} isActive={section === key} component="button" onClick={() => navigate(key)} icon={<Icon />} data-testid={'nav-' + key}>{label}</NavItem>)}</NavList></Nav><div className="gr-sidebar-footer"><Label color="teal" icon={<ServerIcon />}>Hardware isolated</Label><p>One Pod, one microVM.</p><p>Closing the console leaves workloads running.</p><p><a href="/assets/generated/licenses.txt" target="_blank" rel="noopener noreferrer">Third-party notices</a></p></div></PageSidebarBody></PageSidebar>;
  let content = null;
  if (view) {
    const pages = {
      overview: <Overview view={view} navigate={navigate} events={records} />, workloads: <Workloads view={view} openExec={openExec} />,
      topology: <Topology view={view} />, networking: <Networking view={view} />, storage: <Storage view={view} />,
      configuration: <Configuration view={view} />, logs: <Logs revision={gaps} />, events: <Events records={records} connection={connection} gaps={gaps} />,
      exec: <Exec key={application} view={view} preferredTarget={preferredTarget} />, diagnostics: <Diagnostics view={view} />,
    };
    content = pages[section];
  }
  return <Page masthead={masthead} sidebar={sidebar} onPageResize={onPageResize} mainContainerId="main-content" className="gr-page" data-console="patternfly-react"><PageSection className="gr-page-heading"><div className="gr-page-eyebrow">APPLICATION CONSOLE</div><div className="gr-title-row"><div><Title headingLevel="h1" size="2xl">{heading}</Title><p className="gr-subtitle">{help[section]}</p></div><Label color="blue">{application || 'No application'}</Label></div><Toolbar><ToolbarContent><ToolbarItem><FormSelect id="apps" aria-label="Application" value={application} onChange={(_, name) => choose(name)} isDisabled={!authorized || !applications.length}>{applications.length ? applications.map(name => <FormSelectOption key={name} value={name} label={name} />) : <FormSelectOption value="" label="No applications" />}</FormSelect></ToolbarItem><ToolbarItem><Button id="refresh" variant="secondary" icon={<SyncAltIcon />} onClick={refresh} isDisabled={!authorized || loading}>Refresh</Button></ToolbarItem><ToolbarItem><span id="status" className="gr-muted" role="status">{loading ? (view ? 'Refreshing snapshot…' : 'Loading snapshot…') : `${applications.length} application(s)`}</span></ToolbarItem></ToolbarContent></Toolbar></PageSection><PageSection variant="secondary" isFilled>{error && <Alert isInline variant="danger" title="Console unavailable">{error}</Alert>}{loading && !view ? <div className="gr-loading"><Spinner size="xl" aria-label="Loading application" /></div> : content || <Blank title="No application selected">Start a supported application with <code>grillo up</code>, then refresh. No workload is provisioned by opening this console.</Blank>}</PageSection></Page>;
}
class Boundary extends React.Component {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <main className="gr-fatal"><Alert isInline variant="danger" title="Unable to render the console">Refresh the page and check daemon compatibility. Workloads are not stopped.</Alert></main> : this.props.children; }
}
createRoot(document.getElementById('root')).render(<React.StrictMode><Boundary><Console /></Boundary></React.StrictMode>);
