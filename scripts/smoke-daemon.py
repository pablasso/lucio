#!/usr/bin/env python3
"""Exercise the built CLI and daemon using an independent, synthetic PV3 peer.

No hardware, fixed ports, Internet, credentials, captures, or installed service.
"""
import json
from pathlib import Path
import re
import selectors
import socket
import struct
import subprocess
import sys
import zlib

ROOT = Path(__file__).resolve().parent.parent
DEVICE = bytes(range(16))


def packet(kind, packet_id, body):
    payload = struct.pack("<I16s4I", 0, DEVICE, kind, packet_id, len(body), 0) + body
    return struct.pack("<5I", 3, 0, 0, len(payload), zlib.crc32(payload)) + payload


def exact(sock, n):
    result = bytearray()
    while len(result) < n:
        chunk = sock.recv(n - len(result))
        if not chunk:
            raise RuntimeError("unexpected screen socket EOF")
        result.extend(chunk)
    return bytes(result)


def frame(sock):
    version, _, direction, length, crc = struct.unpack("<5I", exact(sock, 20))
    assert version == 3 and direction == 1 and 0 < length < 4 << 20
    payload = exact(sock, length)
    assert zlib.crc32(payload) == crc
    return payload


def first_image_block(payload):
    index, _, compressed, plain, _, _ = struct.unpack("<6I", payload[:24])
    assert index == 0 and 36 <= plain <= 4800 and compressed <= len(payload) - 24
    data = payload[24:24 + compressed]
    out, pos = bytearray(), 0
    while pos < len(data):
        token = data[pos]
        pos += 1
        length = token >> 4
        if length == 15:
            while True:
                extra = data[pos]
                pos += 1
                length += extra
                if extra != 255:
                    break
        out.extend(data[pos:pos + length])
        pos += length
        if pos == len(data):
            break
        distance = int.from_bytes(data[pos:pos + 2], "little")
        pos += 2
        length = (token & 15) + 4
        if (token & 15) == 15:
            while True:
                extra = data[pos]
                pos += 1
                length += extra
                if extra != 255:
                    break
        assert 0 < distance <= len(out) and len(out) + length <= plain
        for _ in range(length):
            out.append(out[-distance])
    assert len(out) == plain
    return out


def stop(proc):
    if proc is None:
        return
    if proc.poll() is None:
        proc.terminate()
    try:
        proc.communicate(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.communicate(timeout=5)


def main():
    lucio = str(Path(sys.argv[1]).resolve())
    daemon = subprocess.Popen(
        [lucio, "serve", "--gateway-listen", "127.0.0.1:0",
         "--http-listen", "127.0.0.1:0", "--ack-timeout", "5s"],
        cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    push = None
    try:
        with selectors.DefaultSelector() as sel:
            sel.register(daemon.stdout, selectors.EVENT_READ)
            if not sel.select(timeout=20):
                raise RuntimeError("daemon did not start")
            line = daemon.stdout.readline()
        match = re.search(r"gateway=(\S+) control=(\S+)", line)
        assert match, line
        gateway, control = match.groups()
        host, port = gateway.rsplit(":", 1)
        with socket.create_connection((host, int(port)), timeout=15) as screen:
            fields = [(0, 3), (0x26, 2880), (0x27, 640), (0x36, 1), (0xffffffff, 0)]
            screen.sendall(packet(3, 1, b"".join(struct.pack("<II", *f) for f in fields)))
            status_ack = frame(screen)
            expected_ack = struct.pack("<6I", 0, 0, 44, 44, 1, 0)
            expected_ack += struct.pack("<I16s4I", 0, DEVICE, 1, 1, 8, 0)
            expected_ack += struct.pack("<II", 1, 0)
            assert status_ack == expected_ack
            push = subprocess.Popen(
                [lucio, "push", "--scene", "examples/hello.json", "--wait",
                 "--server", "http://" + control],
                cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
            )
            image = first_image_block(frame(screen))
            assert image[4:20] == DEVICE
            kind, packet_id = struct.unpack_from("<II", image, 20)
            assert kind == 5 and packet_id != 0
            screen.sendall(packet(1, packet_id, struct.pack("<II", 1, 0)))
            out, err = push.communicate(timeout=20)
            assert push.returncode == 0, (out, err)
            result = json.loads(out)
            assert result["state"] == "acked" and result["ack"]["matched"]
            assert result["ack"]["packet_id"] == packet_id and result["update_id"]
            status = subprocess.run(
                [lucio, "status", "--json", "--server", "http://" + control],
                check=True, capture_output=True, text=True, timeout=10,
            )
            snapshot = json.loads(status.stdout)
            assert snapshot["connected"] and snapshot["display_state"]["partial_eligible"]
            assert result["update_id"].startswith(snapshot["run_id"] + "-")
        daemon.terminate()
        daemon.communicate(timeout=10)
        assert daemon.returncode == 0, daemon.returncode
    finally:
        stop(push)
        stop(daemon)
    print("Built CLI → private IPC → synthetic PV3 screen → correlated ACK: passed")


if __name__ == "__main__":
    main()
