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
import { Topology } from './topology.jsx';
import { readiness, guestMemory } from './presentation.mjs';

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
function MemoryGauge({ vm }) {
  const memory = guestMemory(vm.guest);
  return <div className="gr-memory-sample"><svg viewBox="0 0 120 120" role="img" aria-label={memory ? `${vm.id}: ${memory.percent}% guest memory used` : `${vm.id}: guest memory unavailable`}><circle className="gr-gauge-track" cx="60" cy="60" r="45" />{memory && <circle className="gr-gauge-fill" cx="60" cy="60" r="45" pathLength="100" strokeDasharray={`${memory.percent} ${100 - memory.percent}`} transform="rotate(-90 60 60)" />}<text x="60" y="58" textAnchor="middle" className="gr-gauge-value">{memory ? `${memory.percent}%` : '—'}</text><text x="60" y="77" textAnchor="middle" className="gr-gauge-caption">{memory ? 'used' : 'unavailable'}</text></svg><strong>{vm.id}</strong><Note>{memory ? `${bytes(memory.used)} used of ${bytes(memory.total)}` : 'No valid guest memory sample'}</Note>{vm.guest && <Note>{new Date(vm.guest.time).toLocaleTimeString()} · {vm.guest.source}</Note>}</div>;
}
function Overview({ view, navigate, events }) {
  const vms = list(view.sandboxes), status = readiness(view);
  const inventory = [
    ['Workloads', list(view.workloads).length, 'workloads', CubesIcon],
    ['MicroVMs', vms.length, 'workloads', ServerIcon],
    ['Running containers', running(view).length, 'workloads', CubesIcon],
    ['Services', list(view.services).length, 'networking', NetworkWiredIcon],
    ['Volumes', list(view.volumes).length, 'storage', DatabaseIcon],
  ];
  return <div className="gr-stack" id="overview"><div className="gr-dashboard">
    <div className="gr-dashboard-column"><Panel title="Application details"><dl className="gr-detail-list"><dt>Application</dt><dd>{view.application}</dd><dt>Source</dt><dd id="source">{view.sourceKind || 'Unavailable'}<p className="gr-muted">{view.sourcePath || 'Source path unavailable'}</p></dd><dt>Isolation model</dt><dd>One Pod · one microVM</dd><dt>Snapshot</dt><dd>{new Date(view.time).toLocaleString()}</dd></dl></Panel><Panel title="Resource inventory">{inventory.map(([name, count, section, Icon]) => <div className="gr-inventory-row" key={name}><span><Icon /> {name}</span><Button variant="link" isInline onClick={() => navigate(section)} aria-label={`View ${name.toLowerCase()}`}>{count}</Button></div>)}</Panel><Panel title="Diagnostics" action={<Button variant="link" isInline onClick={() => navigate('diagnostics')}>View all</Button>}><strong className="gr-summary-number">{list(view.diagnostics).length}</strong><Note>Reported diagnostics. A zero count is not proof of health.</Note></Panel></div>
    <div className="gr-dashboard-column"><Panel title="Workload readiness" action={<Button variant="link" isInline onClick={() => navigate('topology')}>Topology</Button>}><div className="gr-readiness"><CheckCircleIcon className={status.complete ? 'gr-ready' : 'gr-muted'} /><div><strong>{status.observed ? `${status.ready}/${status.observed} observed microVMs ready` : 'No microVMs observed'}</strong><Note>{status.desired} desired replicas · {status.complete ? 'Observed readiness matches desired count' : 'Readiness incomplete or no desired replicas'}</Note></div></div></Panel><Panel title="Guest memory"><div className="gr-gauges">{vms.slice(0, 4).map(vm => <MemoryGauge key={vm.id} vm={vm} />)}</div>{!vms.length && <Note>No observed microVMs to sample.</Note>}<Note>Per-microVM guest memory only, not host capacity or VMM RSS. {vms.length > 4 && 'Showing first four observations.'} CPU percentage, host disk and network utilization are unavailable.</Note></Panel><Panel title="Published routes" id="routes" action={<Button variant="link" isInline onClick={() => navigate('networking')}>View all</Button>}>{list(view.routes).length ? list(view.routes).map((r, i) => <div className="gr-route" key={i}><strong>{r.hostname}{r.path}</strong><Note>Service {r.service}</Note><RouteLink route={r} /></div>) : <Note>No published routes.</Note>}</Panel></div>
    <div className="gr-dashboard-column"><Panel title="Recent activity" action={<Button variant="link" isInline onClick={() => navigate('events')}>View all</Button>}>{events.length ? events.slice(-6).reverse().map(e => <div className="gr-activity" key={e.seq}><Label color="teal" isCompact>{e.kind}</Label><Note>{e.resource || 'Daemon'}</Note><p>{e.message || 'No message'}</p></div>) : <Note>No retained daemon events. Sparse activity does not imply a healthy or silent workload.</Note>}<Note>Global daemon events, not application-only activity.</Note></Panel><Panel title="Runtime boundary"><Label color="teal">Experimental Linux / KVM</Label><Note>No Kubernetes control plane. Closing this console leaves workloads running.</Note><Button variant="link" isInline onClick={() => navigate('workloads')}>Inspect microVMs</Button></Panel></div>
    </div><Metrics view={view} /></div>;
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
  const masthead = <Masthead className="gr-masthead" display={{ default: 'inline' }}><MastheadMain><MastheadToggle><PageToggleButton variant="plain" aria-label="Toggle navigation" isSidebarOpen={sidebarOpen} onSidebarToggle={() => setSidebarOpen(v => !v)}><BarsIcon /></PageToggleButton></MastheadToggle><MastheadBrand><MastheadLogo component="div"><span className="gr-brand-mark">g</span><span className="gr-brand">grillo</span></MastheadLogo></MastheadBrand></MastheadMain><MastheadContent><span className="gr-masthead-caption">Application console</span><span className="gr-header-context"><ServerIcon /> Local runtime</span><Label variant="outline" className="gr-release">Experimental</Label></MastheadContent></Masthead>;
  const sidebar = <PageSidebar isSidebarOpen={sidebarOpen} inert={!sidebarOpen}><PageSidebarBody><div className="gr-sidebar-context"><ServerIcon /><div><strong>Local workspace</strong><span>Linux / KVM</span></div></div><div className="gr-nav-heading">APPLICATION</div><Nav aria-label="Console navigation"><NavList>{sections.map(([key, label, Icon]) => <NavItem key={key} itemId={key} isActive={section === key} component="button" onClick={() => navigate(key)} icon={<Icon />} data-testid={'nav-' + key}>{label}</NavItem>)}</NavList></Nav><div className="gr-sidebar-footer"><Label color="teal" icon={<ServerIcon />}>Hardware isolated</Label><p>One Pod, one microVM.</p><p>Closing the console leaves workloads running.</p><p><a href="/assets/generated/licenses.txt" target="_blank" rel="noopener noreferrer">Third-party notices</a></p></div></PageSidebarBody></PageSidebar>;
  let content = null;
  if (view) {
    const pages = {
      overview: <Overview view={view} navigate={navigate} events={records} />, workloads: <Workloads view={view} openExec={openExec} />,
      topology: <Topology key={application} view={view} navigate={navigate} openExec={openExec} />, networking: <Networking view={view} />, storage: <Storage view={view} />,
      configuration: <Configuration view={view} />, logs: <Logs revision={gaps} />, events: <Events records={records} connection={connection} gaps={gaps} />,
      exec: <Exec key={application} view={view} preferredTarget={preferredTarget} />, diagnostics: <Diagnostics view={view} />,
    };
    content = pages[section];
  }
  return <Page masthead={masthead} sidebar={sidebar} onPageResize={onPageResize} mainContainerId="main-content" className="gr-page" data-console="patternfly-react"><PageSection className="gr-page-heading"><div className="gr-page-eyebrow">Workspace / {application || 'Applications'} / {heading}</div><div className="gr-title-row"><div><Title headingLevel="h1" size="2xl">{heading}</Title><p className="gr-subtitle">{help[section]}</p></div><Label color="teal">{application || 'No application'}</Label></div><Toolbar className="gr-context-toolbar"><ToolbarContent><ToolbarItem><span className="gr-context-label">Application</span></ToolbarItem><ToolbarItem><FormSelect id="apps" aria-label="Application" value={application} onChange={(_, name) => choose(name)} isDisabled={!authorized || !applications.length}>{applications.length ? applications.map(name => <FormSelectOption key={name} value={name} label={name} />) : <FormSelectOption value="" label="No applications" />}</FormSelect></ToolbarItem><ToolbarItem><Button id="refresh" variant="secondary" icon={<SyncAltIcon />} onClick={refresh} isDisabled={!authorized || loading}>Refresh</Button></ToolbarItem><ToolbarItem><span id="status" className="gr-muted" role="status">{loading ? (view ? 'Refreshing snapshot…' : 'Loading snapshot…') : `${applications.length} application(s)`}</span></ToolbarItem></ToolbarContent></Toolbar></PageSection><PageSection variant="secondary" isFilled>{error && <Alert isInline variant="danger" title="Console unavailable">{error}</Alert>}{loading && !view ? <div className="gr-loading"><Spinner size="xl" aria-label="Loading application" /></div> : content || <Blank title="No application selected">Start a supported application with <code>grillo up</code>, then refresh. No workload is provisioned by opening this console.</Blank>}</PageSection></Page>;
}
class Boundary extends React.Component {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <main className="gr-fatal"><Alert isInline variant="danger" title="Unable to render the console">Refresh the page and check daemon compatibility. Workloads are not stopped.</Alert></main> : this.props.children; }
}
createRoot(document.getElementById('root')).render(<React.StrictMode><Boundary><Console /></Boundary></React.StrictMode>);
