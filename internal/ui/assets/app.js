// SPDX-License-Identifier: Apache-2.0
// The web console never renders untrusted strings as HTML: all data is written
// with textContent.

const statusEl = document.getElementById("status");
const appsEl = document.getElementById("apps");
const eventsEl = document.getElementById("events");

async function bootstrap() {
  const params = new URLSearchParams(location.hash.slice(1));
  const token = params.get("token");
  if (token) {
    const resp = await fetch("/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
    history.replaceState(null, "", location.pathname);
    if (!resp.ok) {
      statusEl.textContent = "Session rejected. Reopen the URL printed by `grillo ui`.";
      return;
    }
  }
  await refresh();
  subscribe();
}

async function refresh() {
  const resp = await fetch("/v1/applications");
  if (!resp.ok) {
    statusEl.textContent = "Not authorized. Open the URL printed by `grillo ui`.";
    return;
  }
  const data = await resp.json();
  statusEl.textContent = data.applications.length + " application(s)";
  appsEl.replaceChildren();
  for (const name of data.applications) {
    const row = document.createElement("div");
    row.className = "item";
    row.textContent = name;
    appsEl.appendChild(row);
  }
}

function subscribe() {
  const source = new EventSource("/v1/events");
  source.onmessage = (event) => {
    eventsEl.textContent += event.data + "\n";
  };
}

bootstrap();
