# Third-party notices

Lucio's original code, scenes, synthetic protocol fixtures, generated preview,
grayscale image, and example PDF are MIT licensed. There is no vendor source,
firmware, device capture, private content, or newspaper scan in the distribution.

## Included in the binaries

| Component | Version / provenance | License text |
| --- | --- | --- |
| Go runtime/standard library | Build toolchain; minimum Go 1.26.8 | [BSD 3-Clause](third_party/licenses/go-bsd.txt) |
| `golang.org/x/image` | v0.46.0 | [BSD 3-Clause](third_party/licenses/go-bsd.txt) |
| `golang.org/x/text` | v0.42.0 | [BSD 3-Clause](third_party/licenses/go-bsd.txt) |
| `golang.org/x/sys` | v0.48.0 | [BSD 3-Clause](third_party/licenses/go-bsd.txt) |
| `go.bug.st/serial` | v1.8.0, Cristian Maglie | [BSD 3-Clause](third_party/licenses/serial.txt) |
| Noto Sans, Serif, and Sans Mono, regular/bold | Six unmodified bundled faces; [Noto](https://github.com/notofonts) | [SIL OFL 1.1](internal/renderer/fonts/OFL.txt) |
| IANA timezone data via Go `time/tzdata` | Embedded in Paperboy from its toolchain | [Public-domain notice](third_party/licenses/timezones.txt) |

The Go and listed `golang.org/x/*` license texts are identical and reproduced
once with their original copyright. Exact linked versions/build settings are
in each release's `BUILDINFO.txt`. Source dependency versions/checksums are in
`go.mod`/`go.sum`. Review these notices when changing dependencies or fonts.

The bundled font files remain available under OFL, not MIT. Their original
notice is retained; no claim is made that the fonts themselves were authored by
Lucio. PDF rasterization may use host-installed fonts, independently of Lucio's
own embedded-font scene renderer.

## Optional dependencies, installed separately

- [pySerial 3.5](https://pypi.org/project/pyserial/3.5/): BSD 3-Clause; used only
  by the Python USB helper, not distributed as part of Lucio binaries.
- [Poppler](https://poppler.freedesktop.org/): GPL-family licenses; `pdftocairo`
  is invoked as a separate installed program by optional Paperboy. Poppler is
  not bundled with Lucio. Its distributor's licensing and updates apply.
- Newspapers and third-party PDFs retain their owners' rights. Downloading a
  document does not relicense it under Lucio's MIT license.

Visionect and Joan are trademarks/names of their respective owners. Compatibility
notes do not imply affiliation, endorsement, a vendor warranty, or a license to
redistribute proprietary firmware/content.
