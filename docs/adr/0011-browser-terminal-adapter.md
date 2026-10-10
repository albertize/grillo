# 0011 — Authenticated browser Pod Terminal adapter

- Status: Proposed (implementation explicitly requested by the user; formal review pending)
- Related tasks: T23, T24
- Supersedes: the deferred interactive browser Terminal limitation in ADR 0010
- Frontend text-only rendering superseded by [0013](0013-xterm-browser-terminal.md);
  bounded authenticated transport remains in use.

## Context

The user explicitly requested an actual browser terminal and authorized browser
verification in a second Chrome instance. Captured non-TTY Exec or CLI guidance is
not an interactive terminal. The daemon/guest already implement stdin, PTY, resize,
exit and cancellation on a private Unix attach channel.

## Options considered

- Keep CLI-only guidance: does not satisfy the request.
- Introduce a WebSocket/terminal dependency: potentially richer emulation but
  adds a dependency and supply-chain surface; no need to change the guest protocol.
- Adapt the existing Unix client through bounded HTTP output/input requests:
  retains existing authentication, cancellation and runtime boundaries, with a
  small text-only terminal rendering subset and explicit limitations.

## Decision

Implement the third option. Browser Terminal opens a fixed guest `/bin/sh` with
TTY/stdin, never a host shell. Creation streams bounded SSE JSON containing a
random session ID, base64 output and explicit exit/error. Authenticated POSTs send
stdin/resize or close the session. Every operation requires the bridge cookie,
exact Origin and JSON content type; session IDs do not replace authentication.
See the [API contract](../../api/local-api.md#browser-terminal-adapter-loopback-bridge-only).

Each output request owns its session. Disconnect, cancellation, leaving the tab,
bridge shutdown or the 30-minute deadline cancels the dedicated attached shell,
not the Pod. Four slots are shared with captured Exec. Input/output chunks are
8 KiB, input queues 16 frames, aggregate output 16 MiB, writes five seconds.
There is no reconnect/replay that could duplicate stdin or silently start a shell.
Failed/truncated streams never become success. Backend failures remain sanitized.

Frontend rendering is escaped text with bounded cursor/erase handling and
scrollback. OSC/DCS requests for links, titles or clipboard are ignored. It is not
a full terminal emulator: colors, alternate/full-screen applications and wide
character alignment remain limited. Ctrl+C/D and cursor keys reach the guest PTY;
paste is bounded and input failures disconnect rather than silently lose keystrokes.

## Evidence

[Chrome/real-KVM report](../experiments/t24-browser-terminal-compose-dependencies.md)
records actual stdin, `test -t`, `stty`/resize, Ctrl+C, shell/child cancellation,
unchanged workload lifetime, security fixtures and failed gate corrections.
Chrome uses a separate test-owned profile/loopback CDP endpoint; existing profiles
and processes are untouched. Firefox remains a separately reported missing tool.

## Consequences and follow-up

No new dependencies, downloads, custom WebSocket implementation, host-file API,
secret reveal or authentication weakening. Existing private Unix/guest attach is
reused. Arbitrary workload stdout is not secret-redacted. A richer VT emulator,
IME/accessibility expansion and long-idle/throughput testing are separate work,
not implied support. T24/D0 product gates remain open.
