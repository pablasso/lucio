# Scenes and rendering

A scene is a `lucio.scene/v1` JSON document. It describes a complete 1440×2560
portrait image. Rendering is offline: local PNG/JPEG assets and six bundled
Noto font faces are the only inputs. Rendering never fetches a URL, consults
system fonts, or contacts the display.

Start with `lucio init > scene.json`, or the examples in `examples/`.
Use `lucio describe` for the compact authoring reference and `lucio primitives`
for machine-readable metadata. `lucio schema` prints the embedded JSON Schema.
The `$schema` URL is an editor hint; Lucio does not fetch it at runtime.

## Coordinates and primitives

The origin is the upper-left corner. All geometry uses integer portrait pixels.
Top-level nodes have globally unique `id`, `type`, and `box: {x,y,w,h}` fields.
Order is paint order. Colors are `#rrggbb`; output is an 8-bit grayscale PNG.

- `text`: bundled `sans`, `serif`, or `mono`, regular/bold; wrapping and explicit
  shrink-to-fit. Missing glyphs and overflow are errors.
- `rect`, `line`, `rule`: fills, borders, and separators.
- `image`: a local PNG/JPEG asset, with `exact` (default), `contain`, `cover`, or
  `stretch` fitting. Resizing/cropping is reported rather than hidden.
- `card`, `list`: common content arrangements.
- `grid`, `stack`, `section`: containers that assign child boxes. Their children
  must omit explicit boxes. Stack children supply a height (or width for a
  horizontal stack) and a nested `node`.

Use `examples/hello.json` and `examples/dashboard.json` as complete examples.
For `examples/image.json`, supply your own `picture.png` using `--asset-root`.

## Assets

Relative paths resolve against `--asset-root`, otherwise the scene's directory.
For stdin without an asset root they resolve against the current directory.
Absolute paths are permitted. An asset root is **not a filesystem sandbox**;
only render scenes and assets you trust, under a suitably restricted account.
There is no URL fetching, data URI, PDF decoder, HTML, or JavaScript renderer.

Source files are limited to 64 MiB, 32 million decoded pixels, and 16384 pixels
per side. Oversized `cover` intermediates are rejected too; use `contain` or
prepare the image upstream. E-paper delivery quantizes the preview to 16 gray
levels, so the PNG is a layout preview, not an exact optical simulation.

## Reports and errors

`lucio render --scene scene.json --out preview.png --report report.json` writes
an image and a report. The report lists source paths, layout issues, final
geometry, and the PNG SHA256. Add `--report-detail` for the flat drawing
operations and their source mapping.

Unknown fields, duplicate IDs/keys, null scalars, off-canvas boxes, missing
assets, clipping, and text overflow fail validation. On failure no new PNG is
written, and a report is written when requested. An existing output file is not
a successful new render; always check the command's exit status and report.

Resource limits are additional to JSON Schema and layout validation: 1 MiB of
JSON, 32 levels of nesting, 16384 value tokens, 4096 bytes per string/key,
numeric magnitude at most 4096, and font sizes 1–512. These prevent accidental
unbounded allocations; they do not make rendering hostile input a sandbox.

The renderer keeps the complete author scene, resolves layout, lowers it to
text/rectangle/line/image drawing operations, and renders those operations.
The public contract is scene JSON → PNG + report, not a Go SDK.
