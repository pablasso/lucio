# Set up a display

Lucio targets one configured 32-inch PP32 display using the PV3 gateway protocol,
with a **1440 × 2560 portrait** canvas. The exercised hardware reports PP32 v1.1,
Joan, firmware/bootloader 6.9.3947. Other devices and firmware versions are not
established compatibility targets. Factory-reset/new-device provisioning has
not been validated.

The local gateway/rendering workflow has been exercised on this configuration.
The refactored release is covered by simulated-screen tests, but still needs a
physical-device acceptance run. Keep an existing working configuration and a
rollback path before changing a screen.

## 1. Install Lucio

From a checkout, with Go 1.26.8 or a newer patched release:

```sh
go build -o bin/lucio ./cmd/lucio
./bin/lucio version
./bin/lucio render --scene examples/hello.json --out /tmp/hello.png
```

Rendering is offline. Fonts are embedded; no font installation, Python, PDF tool,
or running daemon is required. macOS builds use cgo for USB port enumeration and
need the Xcode command-line tools. Linux builds do not need cgo.

## 2. Run a gateway on the display's LAN

```sh
./bin/lucio serve
```

Defaults are TCP `0.0.0.0:11113` for the screen and HTTP
`127.0.0.1:18080` for **private local IPC**. Permit the screen to reach the gateway
port through your host firewall. Reserve the server's LAN address so it does not
change underneath the screen. Neither endpoint is suitable for Internet exposure.

The screen initiates the TCP connection. Lucio does not discover or configure it
over the network. It does not send a saved image at startup or on reconnection.

## 3. Inspect/configure USB, only if necessary

Skip this step if the screen already connects to the intended gateway. USB is a
setup tool, not a requirement for normal display updates.

**Two implementations:** the Python helper's serial configuration workflow was
physically validated on the target hardware. The Go `lucio usb` implementation has
unit/build coverage only; do not treat it as a proven single-binary provisioning
workflow. Neither establishes support for unconfigured/factory-reset devices.

Create an isolated Python environment for the optional helper:

```sh
python3 -m venv .venv
.venv/bin/pip install -r tools/requirements.txt
.venv/bin/python tools/usb-setup.py --list
```

Use the actual USB serial port printed by `--list`. On macOS prefer the
`/dev/cu.usbserial-*` port; the exercised adapter is an FTDI FT232R (0403:6001).
Linux typically exposes it as `/dev/ttyUSB*`. On Linux the invoking user
may need serial-device group membership. Do not run the network daemon as root
just to gain USB access.

Inspect first (substitute `USB_PORT`):

```sh
.venv/bin/python tools/usb-setup.py --port USB_PORT \
  --cmd help --cmd uuid_get --cmd fw_version_get \
  --cmd conn_type_get --cmd conn_type_list --cmd wifi_conf_get \
  --cmd server_tcp_get --cmd ipv4_conf_get
```

The helper uses 115200 baud, CRLF command lines, a short settle/drain interval,
and reads until idle. Help/available commands depend on firmware. Output redaction
is **best effort**, enabled by default; inspect output before sharing it. Avoid
`--show-secrets` and interactive sessions when recording a terminal. Terminal echo,
process arguments, shell history, and your command file are outside redaction.

On the tested configured device the gateway commands were:

```text
server_tcp_set SERVER_LAN_IP 11113
server_tcp_get
wifi_conf_get
ipv4_conf_get
```

These change the device configuration. Replace the placeholder and save those
commands in a private file only when deliberately changing configuration. Keep a
record of previous settings securely; do not publish it. Then:

```sh
chmod 600 /path/to/private-setup.txt
.venv/bin/python tools/usb-setup.py --port USB_PORT --file /path/to/private-setup.txt
```

If Wi-Fi needs changing, use the device's help and confirm the exact firmware's
syntax. The tested CLI uses `wifi_conf_set SSID PASSWORD wpa2 0`; do not put
passwords into shell arguments or a repository. Use a 2.4 GHz WPA2/AES network,
not WPA3-only, with a password no longer than 31 characters. Do not assume quoted
SSIDs/passwords containing spaces are supported.

**Inspect `wifi_conf_get` and `server_tcp_get` before persisting.** Once they show
the intended values, run a separate private file containing `flash_save` and
`reboot`. A reboot may disconnect USB before a final response is read. Verify
DHCP/connectivity and a new status in Lucio; if needed, restore your recorded
settings over USB with the same inspect → save → reboot process. Delete the
private command files afterward. Lucio's gateway is unencrypted and intended
only for a trusted LAN.

For exploratory use of the not-yet-hardware-validated Go implementation:

```sh
./bin/lucio usb --list
./bin/lucio usb --port USB_PORT --cmd help
```

## 4. Inspect and push

In another terminal on the server:

```sh
./bin/lucio status
./bin/lucio devices
./bin/lucio push --scene examples/hello.json --wait
```

`status` is cached: it does not wake/query the screen. Wait for a valid status
before pushing. Successful upload or socket writing is not a display ACK;
`--wait` follows the exact update ID until its matching ACK or failure. Even a
device ACK is not optical verification of the panel.

The first identified device is pinned for the daemon run. For a permanent
installation, set `serve --device-id DEVICE_ID` using `devices --json` to inspect
it. The identifier is **not authentication**.

## 5. Service installation and isolation

See [operations](operations.md) for systemd installation, SSH control, logs,
updates, and rollback. Test a full image and reconnect behavior manually before
scheduling unattended content.

Lucio can replace the vendor server software for the tested configured screen.
It does **not** prove that firmware never contacts outside services. If that
matters, use a correctly enforced network/VLAN egress policy and verify it from
an observation point that covers all traffic paths. Software compatibility is
not a network-isolation guarantee.
