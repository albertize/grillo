# T23/T24 xterm-compatible browser terminal

## Scope

The user rejected the previous preformatted display/separate input box as unlike
OpenShift. Replace that VT subset with xterm.js on the existing real guest PTY,
not captured Exec or a host shell. Dependency direction and style-only CSP
integration are recorded in [ADR 0013](../adr/0013-xterm-browser-terminal.md).
T24/D0 overall remain BLOCKED; no full OpenShift or terminal-app parity claim.

## Implementation and security

- Exact MIT pins: `@xterm/xterm` 5.5.0, `@xterm/addon-fit` 0.10.0, npm integrity
  lock; installed with explicit `make ui-deps`, lifecycle scripts disabled.
  Notices are included in the embedded license file. No runtime Node or CDN.
- Keyboard/paste/IME/selection are integrated into the terminal. ANSI colors,
  cursor/erase, alternate screen and CJK display use the upstream emulator.
  Measured rows AND columns propagate to the PTY; the panel can be resized
  vertically. Fixed guest command sets TERM and executes interactive `/bin/sh`.
- Legacy binary mouse input has a separate strict-base64 field, with mixed and
  invalid payload rejection and 8 KiB decoded bounds. Normal input preserves
  UTF-8 boundaries when bracketed paste needs two chunks. Paste is capped at 8 KiB.
- Each parser write is awaited before more output is read, with cancellation;
  frontend output is capped at 16 MiB as well as the existing server limit.
  Scrollback is 1000 lines, geometry at most 300 rows/500 columns. Four shared
  Exec/Terminal slots, 16 input frames, five-second writes and 30-minute sessions
  remain. No replay/reconnect or duplicate shell creation.
- Fresh style-only nonce per HTML response; scoped document override authorizes
  only xterm-created styles. No real-document patch, script nonce, unsafe-inline
  or eval. Font/theme options are trusted constants. Workload output never becomes
  HTML. OSC title/icon/link/clipboard and window effects are disabled; user
  selection/copy/paste is not replaced by output-driven host actions.
- Cookie/Origin/Host checks and exact Pod/container targeting remain unchanged.
  Disconnect/view departure/page closure cancels the shell, never the Pod.

## Actual verification

Same Linux/amd64 development host and tool versions as the
[preceding real-KVM delivery](t24-browser-terminal-compose-dependencies.md):
Go 1.27.2, Node 24.18.0/npm 11.16.0, Chrome 155.0.8059.39, SELinux Permissive.
Browser automation uses its own Chrome profile/process and loopback debugging;
existing user browser processes/profiles are untouched.

```sh
make ui-deps
env -u GRILLO_HELM_BINARY PATH=/usr/local/go/bin:$PATH make check
PATH=/usr/local/go/bin:$PATH make test-ui-browser
env -u GRILLO_HELM_BINARY PATH=/usr/local/go/bin:$PATH make test-ui
cd web && npm audit --json
```

- `make check` PASS: 18 frontend tests, build/fingerprint, formatting, vet,
  Go/script unit and race tests, binaries and module policy checks. Tests include
  scoped nonce/no script permission, UTF-8/input bounds, parser cancellation,
  fresh per-document CSP metadata and unchanged raw binary input/rejection.
- Fixture Chrome browser PASS: direct integrated focus, colors/alternate-screen
  entry and normal-buffer restoration, CJK output, literal HTML safety, disabled
  output title/links, existing themes/navigation/Exec/Terminal/lifetime and CSP.
  Firefox SKIP: executable unavailable.
- Final real daemon/Chrome/KVM gate PASS in 37.65 seconds (gate duration, not a
  benchmark). Guest rebuilt by the established `make test-ui` target. Verified
  `test -t`, TERM, stty columns on viewport change and rows on panel height change,
  alternate screen/color/cursor output, actual BusyBox vi launch/insert/Escape/
  write/quit and saved-file contents, Ctrl+C of a foreground child and resumed
  shell. Escape/Ctrl+C use native Chrome input events, not the old key encoder.
  Existing checks confirm shell/child cleanup and unchanged workload VMM PIDs
  after bridge/browser closure. CLI interactive/metrics/artifact checks also PASS.
- Explicit npm audit: zero currently reported vulnerabilities. This verifies
  current registry advisories, not upstream provenance or complete security.
- Node syntax, relative Markdown targets and diff checks PASS.

## Failed trials and corrections

1. The initial xterm 6.0.0/fit 0.11.0 trial sent new resize-event bookkeeping
   fields into the strict API, producing HTTP 400. Only typed rows/cols are now
   extracted; unknown fields are still rejected rather than weakening decoding.
2. That trial then produced two real CSP style-element violations: 6.0.0's
   viewport/global scrollbar code bypasses the scoped document override. Rejected
   the trial and pinned the compatible 5.5.0/0.10.0 pair; CSP was not changed to
   unsafe-inline and global DOM functions were not patched. Future upgrades need
   the same security/browser gate.
3. First real vi test sent Escape and `:wq` in one artificial paste. BusyBox's
   escape-sequence timing did not treat it like separate keystrokes, and the
   save-result assertion failed. Separate native Escape, wait, then command mode
   input reproduces actual interaction; the rerun and final native-input gate
   passed. The failed KVM run was not counted as success.

## Limitations and next steps

Not all terminal applications, Unicode versions/emoji, mouse modes, mobile IME,
copy/accessibility or throughput/long-idle sessions have exhaustive browser or
hardware coverage. Binary mouse byte preservation is a transport unit test, not
an end-to-end mouse-application claim. No reconnect/replay. Firefox still missing.
Formal ADR review and existing T24/D0 hardening/clean-host gates remain open.

Already running bridges retain their old embedded frontend until restarted.
The KVM target rebuilds shared development guest artifacts; a daemon started
before their replacement retains its old manifest and must also be restarted
without stopping Pods (see daemon lifetime contract). Do not disable artifact
verification, remove state or use application `down` merely to refresh assets.
