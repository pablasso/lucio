#!/bin/sh
# Only offline files and loopback simulated-screen traffic; never a real screen.
set -eu
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' 0
trap 'exit 1' HUP INT TERM
lucio=${LUCIO_BIN:-./bin/lucio}
paperboy=${PAPERBOY_BIN:-./bin/paperboy}
"$lucio" version --json > "$work/version.json"
"$lucio" schema > "$work/schema.json"
for name in hello dashboard image; do
  "$lucio" render --scene "examples/$name.json" --out "$work/$name.png" --report "$work/$name.report.json"
done
"$lucio" init --template dashboard | "$lucio" render --scene - --out "$work/stdin.png"
python3 scripts/smoke-daemon.py "$lucio"
if command -v pdftocairo >/dev/null 2>&1; then
  "$paperboy" --lucio "$lucio" --pdf examples/page.pdf --no-push --render-size 256 --out "$work/page.png"
else
  echo 'Skipping optional PDF smoke check: pdftocairo not installed'
fi
echo 'Offline CLI and simulated-screen smoke checks passed'
