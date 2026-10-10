# Grillo visual assets

## README banner

- `grillo-banner.svg`: portable 1024 × 256 banner with outlined lettering.
- `grillo-banner.png`: 2048 × 512 high-density export.
- `grillo-banner.source.svg`: editable composition with live text.

The original composition uses a petrol gradient, faint outlined leaves and the
user-supplied `g-icon/g-foglia.svg` artwork beside the lowercase Grillo wordmark.
The layout reference is not copied artwork. Lettering uses Red Hat Display Bold;
its existing [SIL OFL 1.1 notice](../web/licenses/redhatdisplay-OFL.txt) is preserved.
The published SVG has no text/font dependency, script or external resource.
Original banner composition is Apache-2.0; no new copyright-holder claim is made
for the supplied icon or illustration.

To regenerate with an already installed Inkscape and Red Hat Display Bold font,
run from the repository root (no automatic font/tool installation):

```sh
inkscape media/grillo-banner.source.svg --export-text-to-path --export-plain-svg --export-type=svg --export-filename=media/grillo-banner.svg
inkscape media/grillo-banner.svg --export-type=png --export-width=2048 --export-filename=media/grillo-banner.png
```

The README uses the outlined SVG. Its HTML width is constrained by the host
Markdown renderer; intrinsic aspect ratio keeps the image proportional. The
cricket illustration and supplied console screenshot remain separate images;
the screenshot is not benchmark evidence. These banner files are documentation
assets, not additional runtime UI assets.
