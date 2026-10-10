# T23/T24 console branding and color theme

## Scope and dependencies

User-requested bounded console presentation change, using the supplied
`media/g-icon/` files and established React/PatternFly embedded pipeline. Existing
T23 bridge/bootstrap/CSP and public projections remain unchanged. T24/D0 overall
are still BLOCKED; this is not a release, full accessibility audit or new runtime
integration claim. No new dependencies, downloads or privilege changes.

## Implementation and boundaries

- Masthead: transparent 32px PNG and 64px high-density image; the adjacent Grillo
  name provides text, so the image is decorative. No text-letter substitute.
- SVG/ICO favicon, Apple touch icon and local webmanifest with supplied 192/512px
  icons. `/favicon.ico` aliases the embedded ICO. This is icon metadata only:
  no service worker, offline support or guaranteed PWA installation.
- Explicit seven-file asset whitelist; build and Go fingerprints include original
  image bytes and HTML. Runtime serves embedded bytes, not the host media tree.
- System/Light/Dark selector in the masthead. The default reads browser preference;
  System subscribes to live changes. Explicit choices override browser changes
  and persist per origin in `grillo.theme`. Storage denial falls back safely to
  an in-memory choice; other same-origin tabs observe storage updates.
- Small early external module applies the theme independently of the main bundle.
  PatternFly's root theme class plus custom dark surfaces/text/SVG maintain the
  current light design. Browser theme-color metadata follows the effective mode.
- Only allowed preference strings are accepted. No secrets in persistence, inline
  script/style, unsafe HTML, external images or CSP relaxation.

## Verification

Environment: same Linux/amd64 development host, Go 1.27.2, Node 24.18.0,
Chrome 155.0.8059.39 as the [preceding delivery](t24-browser-terminal-compose-dependencies.md).
The browser harness creates a separate owned profile/process and does not reuse
or close the user's Chrome instance. Its backend is the deterministic fixture,
not a new KVM run.

Commands:

```sh
env -u GRILLO_HELM_BINARY PATH=/usr/local/go/bin:$PATH make check
PATH=/usr/local/go/bin:$PATH make test-ui-browser
GRILLO_UI_SCREENSHOT=/tmp/grillo-theme.png PATH=/usr/local/go/bin:$PATH make test-ui-browser
node --check scripts/ui-browser-smoke.mjs
git diff --check
```

- Routine check PASS: frontend unit tests (18 total), build/fingerprint check,
  formatting, vet, Go/script unit tests, race tests, binary builds and module checks.
- Chrome browser smoke PASS: rendered supplied icon, manifest/favicon requests,
  dark initial browser preference, manual Light override of a dark browser,
  manual Dark persistence after authenticated reload, Dark override of a light
  browser, live System switching in both directions, custom card/topology colors,
  mobile inspector/control overflow and eventual link-button colors. Existing
  navigation, log/Exec/Terminal, closure, local fonts and CSP checks still pass.
- Optional dark desktop/mobile screenshots were written under `/tmp` and inspected;
  no generated screenshot is added to the repository.
- Firefox SKIP: executable unavailable. Real KVM tests not rerun: this change only
  affects presentation/static assets; prior terminal/dependency evidence remains
  in the preceding report. No benchmark or full contrast/accessibility campaign.
- Relative Markdown link targets and final diff checks PASS.

The initial browser run failed because its new assertion required an unselected
node's dark fill even while the existing keyboard workflow retained visible
focus. The assertion was corrected to recognize normal/focused/hover dark fills
and additionally verify canvas/text colors; reruns passed. Screenshots now wait
for link-button color transitions rather than capturing their light-to-dark
intermediate frame. No rendering defect was hidden by this correction.

## Remaining work

Verify Firefox on a host with it installed; extended visual/accessibility review
is not claimed. Existing bridges retain old embedded assets until restarted;
restarting presentation does not require stopping workloads. Retain all existing
T24/D0 runtime, security and clean-host blockers.
