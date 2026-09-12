# Architecture

Lucio is one executable with independent offline commands and a long-running
single-screen daemon. The optional Paperboy executable is a content client.
There is no database, plugin framework, browser renderer, persistent desired
image, automatic content fetch, or startup/reconnect image replay.

## Boundaries

- `internal/renderer`: JSON + local assets → grayscale PNG + validation report.
  Author scenes resolve into layouts and flat core drawing operations.
- `internal/control`: same-version private localhost IPC client and wire models.
  HTTP is an implementation detail, not a supported remote REST contract.
- `internal/daemon`: the sole owner of live sockets, packet IDs, pending updates,
  display baseline, and cached status. It does not render scenes.
- `internal/delivery`: pure full/partial selection and conservative geometry.
- `internal/pv3`: framing, supported panel mapping, raw LZ4 image encoding.
- `internal/telemetry`: optional bounded local JSONL and offline analysis.
- `internal/usb`: optional direct serial configuration, independent of the daemon.

## Device ownership

The screen connects to the LAN gateway (TCP 11113). A connection must send a
valid status before replacing an existing session. The first device identity
is pinned for the daemon's lifetime, unless an explicit device ID was supplied.
Other identities are rejected. A device ID is **not authentication**. An attacker
on the trusted network could imitate it; restrict gateway access externally.
At most eight unidentified connections are being read at once, with deadlines.

## Image transaction

1. One bounded PNG upload is decoded at a time. Dimensions are checked first.
2. Under the daemon state lock, the complete immutable target is reserved for
   an update. If another update is in flight, return `busy`; there is no queue.
3. The daemon selects full delivery, or conservative partial delivery against
   the current connection's ACKed framebuffer. The whole target remains the
   source of truth. An unchanged target still falls back to full delivery.
4. The writer sends the frame. Status/control writes share a socket write lock.
5. Only a successful, matching ACK after a successful write commits the target
   as the new baseline. An early ACK is held until the writer confirms success.
6. Write failure, timeout, NACK, shutdown, or connection replacement terminates
   the operation. Uncertain transfers close the socket; the next session starts
   without a partial baseline. Late or stale-connection ACKs cannot revive it.

An autonomous timer bounds sending + ACK waiting. It does not depend on health
polling. IDs include a random daemon instance ID, connection ID, and packet ID.
The daemon retains the last 32 operation results without their image buffers.
Waiting uses that operation's completion signal, never a global `last_ack`.
A client timeout or lost HTTP response does not roll back an accepted transfer.

Status reports are serialized while holding the state lock, then written to
HTTP after unlocking. Mutable pointers never escape to concurrent encoders.
A later status CRC disagreement invalidates partial eligibility. CRC and ACK
are protocol evidence, not proof of physical appearance.

## Lifetime and storage

Both listeners are bound before work starts. Cancellation stops acceptance,
fails pending work, closes screen connections, shuts down HTTP, joins workers,
and closes the optional event log. The physical e-paper image is not cleared.

No image is restored or sent on startup/reconnect. The next explicit submission
establishes a full baseline. Process logs go to stdout/stderr. Explicit event
logging fsyncs JSONL events and rotates them; it is local diagnostics, not
analytics sent to any service. A sticky logging error is visible in status.
