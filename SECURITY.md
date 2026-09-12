# Security

Lucio is experimental software for a **trusted local machine and restricted
screen LAN**, not an Internet-facing service.

## Boundaries

- PV3 has no cryptographic authentication or built-in encryption. Device IDs
  and CRCs identify/correlate state; they do not authenticate a peer. Restrict
  gateway access externally. A compromised LAN peer can impersonate a screen.
- Control binds to literal loopback IP by default. Client-header, Host, and
  Origin checks reduce browser-based abuse; they are not user authentication.
  Any local process able to reach it can control the display. Use SSH, not a
  publicly exposed reverse proxy, for remote administration.
- Scene assets are local filesystem reads. Asset roots are not sandboxes.
  Render trusted scenes under an appropriately restricted account. Input limits
  reduce accidental/resource abuse but do not establish hostile-input safety.
- Optional Paperboy downloads untrusted PDFs and invokes Poppler. Keep Poppler,
  OS libraries, CA certificates, Python dependencies, and Go builds patched.
  A process deadline and image limits do not constrain every native PDF parsing
  allocation; use OS limits and isolation.
- USB commands can change persistent Wi-Fi/server settings. Secret redaction is
  best effort; it cannot protect terminal echo, argv, history, or command files.
- Event logs stay local but contain device/network metadata and timing. Do not
  post them or renderer reports without review. Logging defaults to off.
- Replacing server software does not prove firmware egress isolation. Verify
  your network policy independently, including bridged/bypass traffic paths.

Release binaries should be built with a currently patched Go toolchain and
scanned with `govulncheck`. Checksums detect corruption, not a compromised release
account; release provenance/signing is not presently provided. The supplied
service units are defense in depth, not a substitute for those boundaries.

## Report a vulnerability

For a security issue, contact the maintainer privately at **me@pablasso.com** with
a minimal synthetic reproducer, affected version/platform, impact, and suggested
mitigation. Do not send credentials or real device captures. If GitHub private
vulnerability reporting is enabled for this repository, that is also suitable.

Only the current main branch/latest release is maintained; no backport or
response-time commitment is established. Use ordinary issues for non-sensitive
bugs, with redacted synthetic examples.
