# Operate Lucio

## Defaults and ownership

| Setting | Default |
| --- | --- |
| Screen gateway | TCP `0.0.0.0:11113` |
| Private control | HTTP `127.0.0.1:18080` |
| Update preference | `full` |
| Daemon send + ACK deadline | `2m` |
| Idle screen read deadline | none; incomplete frames are still timed out |
| Event logging | off |
| Saved image / queue / reconnect replay | none |

The daemon owns one screen. The first valid identity is pinned for the process
run; `--device-id` pins it explicitly across restarts. Another identity is
rejected. A reconnect from the pinned identity replaces its previous connection
and invalidates the partial baseline. IDs are not authentication: restrict LAN
access to the gateway.

Only one upload is decoded and one image transaction is active at a time.
Competing publishers receive `busy`, not a hidden queue. Prefer one scheduler.
An offline/not-ready screen yields an error; the daemon does not store work for
later. A process restart leaves the physical e-paper image alone.

Use `lucio serve --help` for limits and deadlines. Raising timeouts does not prove
an update succeeded. A client deadline is separate from the daemon deadline;
losing a response or canceling the client does not undo an accepted transfer.

## Full and partial updates

```sh
lucio push --scene page.json --wait                 # full (default)
lucio push --scene page.json --update partial --wait
lucio push --scene page.json --update auto --wait
```

`partial` and `auto` currently share conservative eligibility/geometry rules.
Both may fall back to full. Partial delivery requires an ACKed framebuffer on
the current connection and a small change representable as supported aligned
strips. Large changes, an unchanged target, a missing baseline, and uncertainty
fall back to full. Inspect `sent.delivery` and `sent.delivery_reason` in the
push JSON, rather than assuming the requested preference was used.

Only a matching update ACK after a successful socket write advances the
baseline. Reconnect, timeout, NACK, unexpected ACK, or contradictory status
invalidates it. There is no automatic anti-ghosting cadence or demonstrated
battery-saving promise. Choose full refresh when in doubt.

## Remote administration

Keep the control listener on loopback. Run the CLI on the server over SSH, or
forward the local control port:

```sh
ssh -N -L 18081:127.0.0.1:18080 user@display-server
# In another terminal on your workstation:
lucio status --server http://127.0.0.1:18081
lucio push --server http://127.0.0.1:18081 --scene page.json --wait
```

Use the same Lucio version at both ends. The CLI renders workstation-local
assets; the daemon receives PNG bytes, not filesystem paths.

`--allow-remote-control` deliberately permits a non-loopback control listener;
it does **not** add authentication, encryption, or a supported public API.
External protection is your responsibility. Browser Origins are rejected, and
private IPC requires the Lucio client header. Those checks mitigate browser
abuse/DNS rebinding, not a malicious local user or network client.

## Install the Linux service

Build/package first; review [`deploy/lucio.service`](../deploy/lucio.service).
These commands install/start software on **the host where you run them**. Do
not run them on a working installation without a backup and a cutover plan.
From a source checkout:

```sh
go build -trimpath -o bin/lucio ./cmd/lucio
sudo install -m 0755 bin/lucio /usr/local/bin/lucio
sudo install -m 0644 deploy/lucio.service /etc/systemd/system/lucio.service
sudo systemctl daemon-reload
sudo systemctl enable --now lucio.service
```

The unit uses an unprivileged dynamic user, a read-only filesystem, private
temporary/device namespaces, and no Linux capabilities. USB configuration runs
separately as an appropriately permitted user. No persistent image directory is
needed. The host must permit the screen to reach TCP 11113. systemd hardening is
not a firewall or a firmware-egress policy.

Use `sudo systemctl edit lucio` for local overrides. To pin an identity:

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/lucio serve --device-id REPLACE_WITH_DEVICE_ID
```

Replace that placeholder before restarting. The complete `ExecStart` line
replaces, rather than appends to, the packaged command.

```sh
sudo systemctl restart lucio
journalctl -u lucio -f
lucio status --json
```

The optional Paperboy service/timer is separate and **not enabled by installing
Lucio**. See [Paperboy](paperboy.md) and review both units before enabling it.

## Logs and battery observations

Process messages go to stdout/stderr (the journal under systemd). No usage
analytics, outbound log service, or persistent image state is configured.
To opt into local event recording, extend the unit with:

```ini
[Service]
LogsDirectory=lucio
LogsDirectoryMode=0700
UMask=0077
ExecStart=
ExecStart=/usr/local/bin/lucio serve --event-log /var/log/lucio/events.jsonl
```

Include your `--device-id` or other overrides on the replacement command too.
The log rotates at 16 MiB with three backups (`events.jsonl.1` through `.3`).
Files are mode 0600, events are fsynced, and truncated pre-existing tails are
refused. Use local storage; synchronous event recording can add disk latency.
Do not attach a second log writer or external copy-truncate rotation to this
file. Stop the service before repairing a malformed tail; keep a private backup
instead of silently discarding it.

A sticky recorder error appears in `status`. An error does not disappear merely
because later writes work; diagnose it and restart deliberately. Events contain
device IDs, LAN addresses, image names/checksums, timestamps, and status values.
They are sensitive local diagnostics, even though image pixels and Wi-Fi
credentials are not recorded by this logger.

Analyze a file you have permission to read:

```sh
lucio battery --events /path/to/events.jsonl
lucio battery --events /path/to/events.jsonl --details
lucio battery --events /path/to/events.jsonl --json
```

Analyze rotated files individually. Each analysis is limited to 32 MiB / 10000
lines; select a smaller complete-line window if necessary. Run windows for one
device when interpreting the aggregate battery percentage/voltage range. ACK
correlation uses run, connection, and packet identity. Status correlations are
observational: gauge noise, charge state, RF conditions, sampling gaps, and other
activity prevent attributing a battery delta to a single refresh. There is no
battery-life estimator or proof that partial delivery saves a particular amount.

## Acceptance and rollback

Before unattended operation, with the screen intentionally available:

1. Record versions and previous gateway settings privately; preview a synthetic
   orientation/grayscale scene offline.
2. Verify identity and readiness, send a full update with `--wait`, and inspect
   the actual panel's orientation, bands, text, and gray levels.
3. Test a small partial change only after a full ACK; verify delivery selection
   and appearance. Do not infer quality from the ACK alone.
4. Reconnect/restart and confirm nothing is replayed automatically, partial
   eligibility is lost, and the next explicit update is full.
5. Test status/timeout diagnostics using a simulated screen before experimenting
   with failure conditions on real hardware.

For a software upgrade, stop scheduled publishers and the service, keep the old
binary/unit, install the new pair, then start the daemon and run the acceptance
checks. Roll back the binary/unit and restart if needed. There is no daemon image
database to migrate. Device configuration changes require their own USB rollback
using securely recorded previous values; a code rollback cannot restore them.

An e-paper image commonly persists without a running daemon, but Lucio cannot
promise firmware behavior in every failure/power state. Preserve a known-good
setup until your own acceptance run is complete.
