# Original, standalone examples

- `hello.json`: orientation marks, text, and four gray levels on a complete page.
- `dashboard.json`: a complete sample dashboard using the scene primitives.
- `image.json` + `picture.png`: a local image fitted without cropping. The 600×400
  picture consists of four 85-step gray bands and their reversed lower half;
  it is synthetic, not a photo or capture.
- `page.pdf`: an original single-page PDF with a title and two rectangles for
  Paperboy's **offline** example/test. It contains no publisher material.

Render JSON scenes with `lucio render --scene examples/hello.json --out /tmp/page.png`.
Use `paperboy --pdf examples/page.pdf --no-push --out /tmp/page.png` only when you
want the optional PDF pipeline (requires Poppler).

All examples are MIT licensed. The README preview is a reduced render of
`hello.json`, not a photograph of a physical display.
