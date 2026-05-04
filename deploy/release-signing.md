# Release Signing (S-4 verifier)

`deploy/install.sh` ships an opt-in detached-signature verifier for the
release archive. The default download flow still uses TLS + SHA-256 only
(TOFU on first deploy); production rollouts should enable signature
verification so a CDN/registry compromise cannot serve a tampered
binary with a matching `.sha256` sidecar.

## What the installer expects

For each release asset `panvex-control-plane-linux-<arch>.tar.gz`, the
installer expects two sidecars next to the archive on the GitHub
release:

```
panvex-control-plane-linux-<arch>.tar.gz
panvex-control-plane-linux-<arch>.tar.gz.sha256
panvex-control-plane-linux-<arch>.tar.gz.minisig   <-- NEW
```

The `.minisig` file is a minisign detached signature (Ed25519). The
public key is short, single-line, RWQ-prefixed, and is embedded in
`deploy/install.sh` as `PANVEX_INSTALL_PUBKEY`. Operators distributing
their own builds may override the key at install time:

```
PANVEX_INSTALL_REQUIRE_SIGNATURE=1 \
PANVEX_INSTALL_PUBKEY='RWQ...' \
bash install.sh
```

When `PANVEX_INSTALL_REQUIRE_SIGNATURE=1` is unset, the verifier is a
no-op and the installer behaves identically to previous releases.

## Generating the release-signing key

This is a one-time operator task. Pick a secure host (offline laptop,
hardware token, or a CI secret store with manual approval — your call,
documented in your runbook).

```bash
minisign -G -p panvex-release.pub -s panvex-release.key
```

minisign will prompt for a password protecting the private key. Store
it in your password manager; you cannot recover it.

The `.pub` file contains the public key on its second line, e.g.:

```
untrusted comment: minisign public key 0123456789ABCDEF
RWQabcdefghijk...
```

The single `RWQ...` line is what goes into `PANVEX_INSTALL_PUBKEY` /
into the placeholder constant at the top of `deploy/install.sh`.

## Signing a release tarball

For each `panvex-control-plane-linux-<arch>.tar.gz` produced by the
release pipeline:

```bash
minisign -S -s panvex-release.key \
  -m panvex-control-plane-linux-amd64.tar.gz
```

This produces `panvex-control-plane-linux-amd64.tar.gz.minisig`.

Upload the `.minisig` alongside the existing `.tar.gz` and `.sha256`
on the GitHub release.

## Publishing the public key

1. Replace the placeholder constant in `deploy/install.sh`:

   ```bash
   : "${PANVEX_INSTALL_PUBKEY:=RWReplaceMeWith...}"
   ```

   with the real `RWQ...` line. Commit and tag.

2. Mirror the key in the repository (`deploy/release-signing.pub`) and
   on the project website / README so operators can verify out of band
   that the embedded constant matches what the project actually
   publishes.

3. Document the key fingerprint (the first 16 hex chars of the
   `untrusted comment:` line) in CHANGELOG.md when introducing it,
   and again whenever it rotates.

## Operator follow-ups (open)

* The constant in `deploy/install.sh` is currently a literal
  placeholder (`RWReplaceMe...`). It MUST be replaced with a real
  release-signing public key before `PANVEX_INSTALL_REQUIRE_SIGNATURE=1`
  can be flipped on by default.
* CI/release tooling does not yet sign archives. Wiring `minisign -S`
  into the release workflow (with the private key stored in CI secret
  storage and gated by manual approval, or, better, signing offline
  and uploading the `.minisig` after the fact) is a separate operator
  task — explicitly out of scope for the S-4 verifier commit.
* Once the key is in place and the workflow signs archives, flip the
  default in operator-facing docs (README install snippet) so new
  installs use `PANVEX_INSTALL_REQUIRE_SIGNATURE=1` from day one.

## Verifying manually

To sanity-check a release without going through the installer:

```bash
minisign -V \
  -P 'RWQ...your-real-pubkey...' \
  -m panvex-control-plane-linux-amd64.tar.gz \
  -x panvex-control-plane-linux-amd64.tar.gz.minisig
```

`Signature and comment signature verified` means the archive matches
the published key.
