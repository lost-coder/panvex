# ADR-002: Release signing — cosign keyless (Sigstore)

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P1-SEC-02

## Context

Phase 1 shipped Panvex binaries and container images without any signature
or provenance metadata. Operators installing the agent or control-plane had
no cryptographic way to verify that a tarball pulled from GitHub Releases
was the artifact our CI actually produced. The audit flagged this under
P1-SEC-02 as a supply-chain gap: a compromised release bucket, a
MITM-against-releases-download flow, or a malicious fork publishing
lookalike assets would all succeed silently. We needed signing that was
(a) verifiable with off-the-shelf tooling, (b) operable without
long-lived signing keys that maintainers would need to rotate and guard,
and (c) reproducible inside the existing GitHub Actions release workflow.

## Decision

Adopt **cosign keyless signing** backed by Sigstore's Fulcio/Rekor. The
release workflow (`.github/workflows/release.yml`) requests an OIDC token
from GitHub Actions, cosign exchanges it for a short-lived Fulcio
certificate bound to the workflow identity, signs each binary and
container, and publishes the signature to Rekor's transparency log.
Verification uses `cosign verify-blob` / `cosign verify` with
`--certificate-identity` and `--certificate-oidc-issuer` pinned to our
workflow. No long-lived private key is ever created or stored.

## Alternatives considered

- **minisign.** Attractive for its simplicity but requires a maintainer to
  hold a private key indefinitely and distribute the public key
  out-of-band. Deferred to a future "production-prep" track if we ever
  need an offline-verifiable signature format not dependent on Sigstore.
- **GPG / OpenPGP.** Rejected: operational burden (keyserver hygiene,
  subkey rotation, revocation certificates) does not match the threat
  model, and tooling UX is poor for end users.
- **Unsigned with checksums only.** Rejected: SHA-256 in a release note
  protects only against accidental corruption, not a compromised
  release asset.

## Consequences

- Verification requires `cosign` on the operator's machine. The install
  docs ship a one-liner and a short rationale.
- The release workflow now needs `id-token: write` permission; any
  downstream fork that re-uses the workflow must grant the same.
- Rekor entries are public, which is desirable for transparency but means
  release timing and artifact digests are observable. No secret content
  is embedded in signatures.
- Future SBOM and SLSA provenance attestations plug into the same cosign
  invocation without a second signing system.
