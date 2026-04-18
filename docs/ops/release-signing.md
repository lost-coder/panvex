# Release Signing — Runbook

Panvex refuses to install any update binary (control-plane or agent) that is
not accompanied by a valid detached signature. This protects the update
subsystem from supply-chain attacks even if a malicious `GitHubRepo` setting,
compromised GitHub token, or MitM diverts the download.

## Trust model

- **Public key (committed)**: `core/internal/security/signing_key.pub`
  - PEM-encoded ECDSA P-256.
  - Embedded into `control-plane` and `agent` binaries via `go:embed`.
  - Changing this file requires cutting a new release — older binaries will
    reject artifacts signed with the new private key until they receive the
    new public key via a binary update signed with the OLD key.
- **Private key (not committed)**:
  - Held in the repository secret `PANVEX_SIGNING_KEY` on GitHub.
  - Used exclusively by the `Release` workflow to produce `.sig` artifacts
    during tag builds.
  - Never written to persistent disk during CI; the release job copies it to
    a mode-0600 temp file, signs, and shreds the temp file on exit.

## Signing format

- Algorithm: ECDSA P-256 over SHA-256.
- Produced by: `openssl dgst -sha256 -sign <key.pem> -out <file>.sig <file>`.
- Output: raw ASN.1 DER (not base64). Cosign-style base64 DER is also accepted
  by the Go verifier (`internal/security.VerifyArtifactBytes`).

## Generating a fresh key pair (dev or rotation)

```bash
# Generate an ECDSA P-256 private key (PKCS#8 PEM).
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out panvex-signing.key

# Extract the corresponding public key.
openssl pkey -in panvex-signing.key -pubout -out panvex-signing.pub
```

## Install a key pair for release builds

1. Replace `core/internal/security/signing_key.pub` with the new public key:
   ```bash
   cp panvex-signing.pub core/internal/security/signing_key.pub
   ```
2. Commit the change. **Ship a release binary signed with the OLD key** first
   so running deployments can update to a binary that embeds the NEW public
   key (bridge release).
3. In GitHub → Settings → Secrets → Actions, set/replace
   `PANVEX_SIGNING_KEY` with the contents of `panvex-signing.key`.
4. After the next release build, verify the signature locally (see below).
5. Delete the private-key PEM from developer machines (`shred -u` or
   platform-equivalent). Retain only the copy in the GitHub secret.

## Rotation procedure (planned)

When rotating without a compromise incident:

1. Generate a new key pair (above).
2. Land a **bridge release** signed with the **old** key that embeds the **new**
   public key. Every deployment that updates through this release now trusts
   both.
3. Replace `PANVEX_SIGNING_KEY` in GitHub Secrets with the new key.
4. Cut the next release. Deployments that applied the bridge will accept it;
   any deployment older than the bridge must install the bridge first.
5. Destroy all copies of the old private key after the bridge rollout
   completes.

## Emergency revocation (compromise)

If the private key is believed to be exposed:

1. **Stop** the release pipeline (disable the `Release` workflow).
2. Generate a new key pair on a clean host.
3. Cut an emergency release signed with the **compromised** key that embeds
   the new public key. (Existing deployments still trust the compromised key
   until they update, so this is the only in-band way to roll them forward.)
4. Rotate `PANVEX_SIGNING_KEY` in Secrets.
5. Announce the incident; any deployments that cannot reach the bridge
   release must be re-provisioned manually with a binary containing the new
   public key.
6. Investigate how the key leaked; tighten access to repository secrets.

## Verifying a release locally

```bash
ARCH=amd64
VERSION=1.2.3

curl -LO "https://github.com/lost-coder/panvex/releases/download/control-plane/v${VERSION}/panvex-control-plane-linux-${ARCH}.tar.gz"
curl -LO "https://github.com/lost-coder/panvex/releases/download/control-plane/v${VERSION}/panvex-control-plane-linux-${ARCH}.tar.gz.sig"

openssl dgst -sha256 -verify core/internal/security/signing_key.pub \
  -signature panvex-control-plane-linux-${ARCH}.tar.gz.sig \
  panvex-control-plane-linux-${ARCH}.tar.gz
# Expected output: "Verified OK"
```

If `openssl` reports a mismatch, **do not install** the artifact. Treat a
mismatch as a potential supply-chain incident and escalate.

## Verifying the SBOM

Every release also publishes a CycloneDX JSON SBOM per architecture plus a
matching cosign signature. The SBOM is produced by `anchore/sbom-action` and
signed with the same ECDSA P-256 key used for the archive.

```bash
ARCH=amd64
VERSION=1.2.3
COMPONENT=control-plane
BASE="https://github.com/lost-coder/panvex/releases/download/${COMPONENT}/v${VERSION}"

curl -LO "${BASE}/panvex-${COMPONENT}-linux-${ARCH}.sbom.json"
curl -LO "${BASE}/panvex-${COMPONENT}-linux-${ARCH}.sbom.json.sig"

# Verify the detached cosign signature.
cosign verify-blob \
  --key core/internal/security/signing_key.pub \
  --signature panvex-${COMPONENT}-linux-${ARCH}.sbom.json.sig \
  panvex-${COMPONENT}-linux-${ARCH}.sbom.json
# Expected output: "Verified OK"
```

Optional static analysis of the SBOM contents (requires `cyclonedx-cli`):

```bash
cyclonedx-cli analyze --input-file panvex-${COMPONENT}-linux-${ARCH}.sbom.json
# or validate the schema
cyclonedx-cli validate --input-file panvex-${COMPONENT}-linux-${ARCH}.sbom.json
```

If signature verification fails, treat the release as untrusted and do not
install it.

## Dev-stage signing keys

During active development the repository ships with a stand-in key pair
generated for local testing. The private key lives outside the repository at
`data/dev-signing-key/panvex-dev-signing.key` (gitignored). Before the first
production deployment, generate a fresh key pair on secure hardware, replace
the public key in the repository, and load the private key into GitHub
Secrets as described above.
