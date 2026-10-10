// SPDX-License-Identifier: Apache-2.0
const text = (id, value) => { document.getElementById(id).textContent = value; };
async function api(path, options) {
  const response = await fetch(path, { ...options, signal: AbortSignal.timeout(10000) });
  if (!response.ok) throw new Error(`API request failed (${response.status})`);
  return response.json();
}
async function refresh() {
  text('status', 'Connecting…');
  try {
    const status = await api('/api/status');
    text('service', status.service); text('hostname', status.hostname);
    text('version', status.goVersion); text('uptime', `${status.uptimeSeconds} seconds`);
    text('status', 'Connected · Nginx → Go backend');
    const notes = await api('/api/notes');
    const entries = notes.map(note => { const li = document.createElement('li'); li.textContent = `${note.id}. ${note.text}`; return li; });
    document.getElementById('notes').replaceChildren(...entries);
  } catch (error) { text('status', error.message); }
}
document.getElementById('refresh').addEventListener('click', refresh);
document.getElementById('note-form').addEventListener('submit', async event => {
  event.preventDefault(); const input = document.getElementById('note');
  const button = event.currentTarget.querySelector('button'); button.disabled = true;
  try { await api('/api/notes', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: input.value }) }); input.value = ''; text('note-status', 'Saved in Go backend memory.'); await refresh(); }
  catch (error) { text('note-status', error.message); }
  finally { button.disabled = false; }
});
refresh();
