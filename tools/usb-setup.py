#!/usr/bin/env python3
"""
Minimal Visionect USB serial CLI helper.

Requires pyserial, preferably in a virtual environment:
  python3 -m venv .venv
  .venv/bin/python -m pip install pyserial

Examples:
  .venv/bin/python tools/usb-setup.py --list
  .venv/bin/python tools/usb-setup.py --port /dev/cu.usbserial-XXXX --cmd help --cmd uuid_get --cmd server_tcp_get
  .venv/bin/python tools/usb-setup.py --port /dev/cu.usbserial-XXXX --file commands.txt --redact

Notes:
- Validated with a Visionect/Joan PP32 screen firmware 6.9.3947 using 115200 8N1.
- Review output before committing logs; Wi-Fi PSKs may appear in command echoes or wifi_conf_get output.
"""

from __future__ import annotations

import argparse
import math
import re
import sys
import time
from pathlib import Path


SECRET_PATTERNS = [
    # wifi_conf_get output in Visionect docs: "PSK: wpa2_pass"
    (re.compile(r"(?im)^(\s*PSK\s*:\s*)(.*)$"), r"\1<redacted>"),
    # Commands in transcripts.
    (re.compile(r"(?im)(wifi_psk_set\s+)(\S.*)$"), r"\1<redacted>"),
    (re.compile(r"(?im)(wifi_conf_set\s+\S+\s+)(\S+)(\s+\S+\s+\S+)"), r"\1<redacted>\3"),
]


def redact_text(text: str) -> str:
    for pattern, replacement in SECRET_PATTERNS:
        text = pattern.sub(replacement, text)
    return text


def require_pyserial():
    try:
        import serial  # type: ignore
        from serial.tools import list_ports  # type: ignore
    except ImportError:  # pragma: no cover
        print("pyserial is required. Suggested setup:", file=sys.stderr)
        print("  python3 -m venv .venv", file=sys.stderr)
        print("  .venv/bin/python -m pip install pyserial", file=sys.stderr)
        raise SystemExit(2)
    return serial, list_ports


def list_serial_ports() -> int:
    _serial, list_ports = require_pyserial()
    ports = list(list_ports.comports())
    if not ports:
        print("No serial ports found.")
        return 1
    for p in ports:
        print(f"{p.device}\t{p.description}\t{p.hwid}")
    return 0


def read_available(ser, idle_seconds: float, max_seconds: float) -> str:
    chunks: list[bytes] = []
    start = time.monotonic()
    last_data = time.monotonic()
    total = 0
    while time.monotonic() - start < max_seconds:
        n = ser.in_waiting
        if n:
            chunk = ser.read(min(n, 4096))
            total += len(chunk)
            if total > 1024 * 1024:
                raise ValueError("serial response exceeds 1 MiB")
            chunks.append(chunk)
            last_data = time.monotonic()
            continue
        if chunks and time.monotonic() - last_data >= idle_seconds:
            break
        time.sleep(0.05)
    return b"".join(chunks).decode("utf-8", "replace")


def run_commands(args: argparse.Namespace) -> int:
    serial, _list_ports = require_pyserial()
    commands: list[str] = []
    commands.extend(args.cmd or [])
    if args.file:
        for line in Path(args.file).read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            commands.append(line)

    if not commands and not args.interactive:
        print("No commands supplied. Use --cmd, --file, or --interactive.", file=sys.stderr)
        return 2

    with serial.Serial(args.port, args.baud, timeout=0.1, write_timeout=2) as ser:
        time.sleep(args.open_delay)
        # Wake/prompt nudge and capture banner/noise.
        ser.write(b"\r\n")
        ser.flush()
        banner = read_available(ser, args.idle, args.read_timeout)
        if banner:
            print(redact_text(banner) if args.redact else banner, end="")

        for cmd in commands:
            printable = redact_text(cmd) if args.redact else cmd
            print(f"\n>>> {printable}", flush=True)
            ser.write((cmd + "\r\n").encode("utf-8"))
            ser.flush()
            out = read_available(ser, args.idle, args.read_timeout)
            print(redact_text(out) if args.redact else out, end="")

        if args.interactive:
            print("\nEntering line mode. Ctrl-D to exit.")
            while True:
                try:
                    cmd = input("> ")
                except EOFError:
                    print()
                    break
                if not cmd:
                    continue
                ser.write((cmd + "\r\n").encode("utf-8"))
                ser.flush()
                out = read_available(ser, args.idle, args.read_timeout)
                print(redact_text(out) if args.redact else out, end="")

    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description="Visionect USB serial CLI helper")
    ap.add_argument("--list", action="store_true", help="list serial ports and exit")
    ap.add_argument("--port", help="serial port, e.g. /dev/cu.usbserial-XXXX")
    ap.add_argument("--baud", type=int, default=115200)
    ap.add_argument("--cmd", action="append", help="command to send; may be repeated")
    ap.add_argument("--file", help="file containing commands, one per line")
    ap.add_argument("--interactive", action="store_true", help="enter simple line-mode after --cmd/--file commands")
    ap.add_argument("--redact", action="store_true", default=True, help="redact likely Wi-Fi PSKs (default)")
    ap.add_argument("--show-secrets", action="store_false", dest="redact", help="UNSAFE: disable output redaction")
    ap.add_argument("--idle", type=float, default=0.5, help="seconds of quiet after data before considering response complete")
    ap.add_argument("--read-timeout", type=float, default=5.0, help="max seconds to wait for each response")
    ap.add_argument("--open-delay", type=float, default=0.5, help="delay after opening serial port")
    args = ap.parse_args()
    for key in ("idle", "read_timeout", "open_delay"):
        value = getattr(args, key)
        if not math.isfinite(value) or value < 0 or value > 60:
            ap.error(f"{key} must be finite and between 0 and 60 seconds")
    if args.read_timeout == 0 or args.baud <= 0:
        ap.error("read-timeout and baud must be positive")

    if args.list:
        return list_serial_ports()
    if not args.port:
        ap.error("--port is required unless --list is used")
    return run_commands(args)


if __name__ == "__main__":
    raise SystemExit(main())
