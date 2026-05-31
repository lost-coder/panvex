# Release Signing (S1)

Panvex release archives are signed with an **ECDSA P-256 / SHA-256**
detached signature using `openssl`. The same key and scheme are used in
three places, so there is exactly one trust anchor to manage:

1. **CI signs** each archive (`.github/workflows/release.yml`).
2. **The in-app updater verifies** downloads against the public key
   embedded in the binary (`internal/security/signature.go` +
   `internal/security/signing_key.pub`).
3. **The install scripts verify** downloads against the same public key,
   embedded as a literal in `deploy/install.sh` and
   `deploy/install-agent.sh` (they are fetched standalone via
   `curl | bash`, so they cannot read a repo file at runtime).

Signature verification in the install scripts is **enabled by default**.
`openssl` is the only dependency, and it is already present on every
supported platform (the installer also uses it to generate keys).

## What CI publishes

For each release asset `panvex-<component>-linux-<arch>.tar.gz`, the
release workflow uploads:

```
panvex-<component>-linux-<arch>.tar.gz
panvex-<component>-linux-<arch>.tar.gz.sha256   # integrity
panvex-<component>-linux-<arch>.tar.gz.sig      # authenticity (ECDSA, raw DER)
```

The `.sig` is produced by:

```bash
openssl dgst -sha256 -sign "$KEY_FILE" -out "${ARCHIVE}.sig" "${ARCHIVE}"
```

where `$KEY_FILE` holds the PEM-encoded ECDSA P-256 private key sourced
from the `PANVEX_SIGNING_KEY` GitHub Actions secret (written to a
mode-0600 temp file and shredded after signing). The output is a **raw
DER** signature — exactly what `openssl dgst -verify` consumes.

## How the install scripts verify

After the SHA-256 check (hash guards integrity, signature guards
authenticity — both must pass), each installer:

1. Writes the embedded public-key PEM to a mode-0600 temp file (or uses
   `PANVEX_INSTALL_PUBKEY_FILE` if the operator set one).
2. Downloads `${archive_url}.sig`.
3. Runs:

   ```bash
   openssl dgst -sha256 -verify "$pubkey" -signature "$sig" "$archive"
   ```

4. Aborts the install on any failure (missing `openssl`, download
   failure, or signature mismatch). Temp files are removed on all paths.

### Operator opt-out and overrides

* `PANVEX_INSTALL_SKIP_SIGNATURE=1` — disable verification entirely. A
  warning is printed to stderr. Not recommended; intended only for
  air-gapped or offline-mirror scenarios where the operator vouches for
  the artifact out of band.
* `PANVEX_INSTALL_PUBKEY_FILE=/path/to/key.pem` — verify against an
  alternate public key instead of the embedded one. For operators who
  build and sign their own archives.

## The signing key

* **Public key** (the trust anchor): `internal/security/signing_key.pub`,
  a PEM ECDSA P-256 key. It is embedded in the binary AND copied verbatim
  into both install scripts as `PANVEX_RELEASE_PUBKEY_PEM`. These three
  copies MUST stay byte-identical.
* **Private key**: stored only as the `PANVEX_SIGNING_KEY` Actions secret.
  It never lives in the repo. Generate it offline:

  ```bash
  openssl ecparam -name prime256v1 -genkey -noout -out panvex-release.key
  openssl ec -in panvex-release.key -pubout -out panvex-release.pub
  ```

  Paste the contents of `panvex-release.key` into the `PANVEX_SIGNING_KEY`
  repository secret, and commit `panvex-release.pub` to
  `internal/security/signing_key.pub` (replacing the existing key) +
  mirror it into both install scripts.

## Key rotation

The in-app updater supports **multiple trusted keys** (see the multi-key
documentation in `internal/security/signature.go`), which makes rotation
non-breaking:

1. Generate a new keypair offline.
2. Add the new public key alongside the current one in
   `internal/security/signature.go`'s trusted-key set, and update
   `PANVEX_RELEASE_PUBKEY_PEM` in both install scripts (the scripts verify
   against a single embedded key, so ship the new key in the same release
   that starts signing with it).
3. Swap `PANVEX_SIGNING_KEY` to the new private key. New releases are now
   signed with it; binaries that embed both keys still trust old archives.
4. Once all in-field binaries have been updated past the rotation point,
   drop the retired public key from the trusted set.

Document the key fingerprint in `CHANGELOG.md` when introducing or
rotating a key so operators can verify out of band.

## Verifying a release manually

```bash
openssl dgst -sha256 \
  -verify internal/security/signing_key.pub \
  -signature panvex-control-plane-linux-amd64.tar.gz.sig \
  panvex-control-plane-linux-amd64.tar.gz
```

`Verified OK` means the archive matches the published key.
