# Paperboy (optional)

Paperboy is a separate content client: fetch a newspaper front-page PDF, render
page one with Poppler, fit it on a white 1440 × 2560 canvas, and ask `lucio push`
to deliver it. The daemon never fetches web content or invokes PDF tools.

It deterministically rotates NY Times, Boston Globe, Chicago Tribune, and SF
Chronicle by calendar day in **America/Chicago**. Timezone data is embedded.
It uses an **unofficial** Freedom Forum daily-PDF URL pattern; availability,
freshness, and continued access are not guaranteed. There is no fallback to
another paper/date if the selected download fails. Respect the source's terms
and publishers' copyrights. No newspaper content is distributed in this project.

## Install and preview

Build both executables:

```sh
go build -o bin/lucio ./cmd/lucio
go build -o bin/paperboy ./cmd/paperboy
```

Install Poppler (`sudo apt install poppler-utils ca-certificates` on Debian/
Ubuntu; `brew install poppler` on macOS). Paperboy finds a sibling `lucio` binary
before searching PATH, or accepts an explicit `--lucio /path/to/lucio`.

Try the synthetic local PDF **without Internet or a screen**:

```sh
./bin/paperboy --pdf examples/page.pdf --no-push --out /tmp/page.png
```

Preview a downloaded front page (this command **uses the Internet**):

```sh
./bin/paperboy --no-push --out /tmp/today.png
```

Without `--no-push`, it submits and waits for an instance-scoped update ACK:

```sh
./bin/paperboy --server http://127.0.0.1:18080 --out /tmp/today.png
```

Full refresh is the default. `--update partial` or `--update auto` expresses a
preference, not a promise; the daemon may fall back to full. A device ACK is not
optical confirmation of the displayed page.

## Failure and resource behavior

- Each run uses a private, unique temporary directory. `--work-dir DIR` changes
  its parent; `--keep-work` retains that run's inputs/scene/report for debugging.
  Do not publish downloaded PDFs or private runtime material with bug reports.
- Fetch happens once per run, with a default two-minute deadline, a 32 MiB
  limit, and a PDF-header check. TLS verification is never disabled.
- The intermediate render is bounded to a longest edge of 5120 pixels
  (`--render-size`), rather than an unbounded height for a chosen width. Page
  fitting preserves the whole page; area resampling and white margins avoid
  cropping the masthead or footer.
- Poppler has a two-minute process deadline. Decoded PNG dimensions and sizes,
  as well as subprocess output, are bounded. **These are not a PDF sandbox:**
  keep Poppler patched, run unprivileged, and use OS memory/CPU controls for
  untrusted documents. The supplied systemd service includes a memory ceiling.
- A failed download/render does not push a blank placeholder. Delivery failures
  retry the same scene after 1, 5, 15, and 45 minutes (five attempts total), without
  re-fetching. Re-running `lucio push` re-renders the retained local scene.
- Delivery is **not exactly-once**. A failed/timed-out attempt might already have
  changed the screen, and retrying can cause another refresh. Cancellation stops
  retries. A missing ACK is uncertainty, not evidence that the screen is blank.
- Per-attempt client limits are separate from the daemon's ACK deadline. The
  CLI requires a matching update ID and ACK before reporting success.

## Schedule deliberately

Preview and deliver manually before installing the optional systemd service and
timer in [`deploy/`](../deploy/). The timer runs once daily at 08:00
America/Chicago and does **not** make up missed runs at boot. Enable it only if
you want daily Internet fetches and physical display updates. After installing
both binaries and Poppler, the explicit opt-in is:

```sh
sudo install -m 0644 deploy/paperboy.service deploy/paperboy.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now paperboy.timer
systemctl list-timers paperboy.timer
```

Disable it with `sudo systemctl disable --now paperboy.timer`; stop
`paperboy.service` too if you want to cancel a currently running job. systemd
prevents its own timer jobs from overlapping; do not run competing manual
publishers.

A cron alternative should use a host-local lock (`flock` on Linux) so a retrying
run cannot overlap the next invocation. Choose a cadence appropriate to the
screen and your battery observations; this project promises no battery-life
improvement from any particular schedule.
