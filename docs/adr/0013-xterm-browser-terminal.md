# 0013 — xterm-compatible interactive browser terminal

- Status: Proposed (implementation explicitly requested by the user; formal review pending)
- Related tasks: T23, T24
- Supersedes: the text-only frontend rendering limitation in [0011](0011-browser-terminal-adapter.md)

## Context and decision

A real guest PTY displayed through a custom preformatted-text renderer and a
separate input box does not provide the OpenShift-like terminal experience the
user requested. Replace the bespoke renderer/keyboard mapping with maintained
`@xterm/xterm` **5.5.0** and `@xterm/addon-fit` **0.10.0**, MIT-licensed upstream
xterm.js packages. Their exact npm pins/integrity lock and upstream notices are
part of the embedded build; installation is the explicit `make ui-deps` step
with scripts disabled. No runtime Node, CDN or additional Go dependency.

The initial 6.0.0/0.11.0 trial was rejected: viewport/global scrollbar styles
bypassed the scoped document override and caused real Chrome CSP violations.
The selected 5.5.0/0.10.0 pins pass that gate without global DOM patching or
unsafe-inline. They are deliberately older compatible versions, not a claim of
latest-version adoption; future upgrades must revalidate strict CSP.

Keep the existing authenticated bounded bridge/Unix/guest PTY transport. Input
comes from the terminal itself, including IME, paste and application cursor keys;
measured rows/columns propagate through resize. The fixed guest shell command
sets `TERM=xterm-256color` and starts `/bin/sh -i`; no untrusted host shell command.
Support ANSI colors/cursor/erase, alternate screen, selection and bounded
scrollback. Binary terminal input uses an explicitly bounded base64 field so
legacy mouse bytes are not corrupted by JSON/UTF-8.

## Security boundaries

xterm's DOM renderer generates dynamic style elements. Do not enable
`unsafe-inline` or authorize scripts: issue a fresh random style-only CSP nonce
per HTML response and expose it as metadata. A scoped `documentOverride` proxy
sets that nonce only on style elements created by xterm. It neither patches the
real document nor assigns nonces to scripts. Font/theme options are fixed trusted
values. Static assets remain confined; cookie/Origin/Host checks are unchanged.

Block OSC title/icon/link/clipboard requests and keep window manipulation
options disabled. Do not attach link-opening providers. User-initiated selection
and copy/paste remain available. Terminal output is parsed by xterm, never
inserted as HTML or executed as JavaScript. No image/clipboard/host-file addons.

Wait for each bounded parser write before reading more frames; cancel pending
writes on disconnect. Preserve four shared slots, 16-frame input queues, 8 KiB
chunks/paste, 16 MiB output and 30-minute sessions. Clamp measured dimensions to
the existing bridge limits. Leaving the tab/closing the bridge cancels this exec,
not the Pod; no reconnect/replay or change to workload ownership.

## Evidence and limitations

See [delivery evidence](../experiments/t23-xterm-terminal.md) for actual checks,
failures, skipped browsers and hardware gates. This does not claim complete
OpenShift parity, complete Unicode/emoji coverage, all terminal applications,
mobile IME/browser accessibility coverage or T24/D0 product hardening.
