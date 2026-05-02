# Panvex Helm Chart

Production-grade Kubernetes deployment for the Panvex control-plane.

## What this chart does

- Deploys the `panvex-control-plane` container as a Deployment with a
  configurable replica count.
- Creates a ClusterIP Service exposing both the HTTP (`8080`) and gRPC
  (`8443`) endpoints.
- Optionally creates an Ingress for the HTTP panel (operators bring their own
  ingress controller).
- Honours `terminationGracePeriodSeconds=60` so the audit-event drain
  finishes before SIGKILL (audit S-04).
- Ships a `PodDisruptionBudget` (`minAvailable: 1`) by default.
- Optionally runs a `bootstrap-admin` Job once via Helm `post-install` hook
  to create the first admin user.

## What this chart does NOT do

- It does NOT bundle Postgres. Bring your own database — managed (RDS, Cloud
  SQL, AlloyDB), in-cluster (CloudNativePG, Crunchy, Zalando), or external.
  Provide the connection string via `storage.dsn` or `storage.existingSecret`.
- It does NOT manage TLS termination. Configure your ingress controller
  (NGINX, Traefik, cert-manager) — the chart accepts inline TLS spec via
  `ingress.tls` for ingresses that consume Kubernetes Secrets directly.
- It does NOT manage the agent gRPC certificate authority. The control-plane
  mints its own server certificate at startup from an embedded CA per the
  existing reverse-mode design.

## Required values

Three values MUST be supplied before installation; the chart fails closed
when they are missing or default.

| Value | Purpose | Failure mode |
|---|---|---|
| `image.repository` + `image.tag` | The pinned panvex-control-plane image | `nil` tag → install creates a pod that ImagePullBackOff |
| `storage.dsn` OR `storage.existingSecret` | Postgres DSN, e.g. `postgres://panvex:...@db.example/panvex?sslmode=require` | Container start-up panics with "empty postgres password" |
| `encryptionKey.value` OR `encryptionKey.existingSecret` | Passphrase for the at-rest envelope-encryption (TOTP secrets, client secrets) | Container fails on first attempt to read an existing PVS2-encrypted row |

The `encryptionKey` MUST be the same passphrase used by every replica and
must persist across helm-upgrade — rotating it without re-encrypting the
database invalidates every encrypted row.

## Quick start

```bash
helm install panvex deploy/helm/panvex \
  --set image.repository=ghcr.io/lost-coder/panvex-control-plane \
  --set image.tag=0.1.0 \
  --set storage.dsn='postgres://panvex:<pw>@db.example:5432/panvex?sslmode=require' \
  --set encryptionKey.value='<32-bytes-of-entropy>'
```

For production, prefer `existingSecret`:

```bash
kubectl create secret generic panvex-db --from-literal=dsn='postgres://...'
kubectl create secret generic panvex-encryption --from-literal=encryption-key='...'

helm install panvex deploy/helm/panvex \
  --set image.repository=ghcr.io/lost-coder/panvex-control-plane \
  --set image.tag=0.1.0 \
  --set storage.existingSecret=panvex-db \
  --set encryptionKey.existingSecret=panvex-encryption
```

## Bootstrap first admin

```bash
kubectl create secret generic panvex-bootstrap \
  --from-literal=password='<strong-password>'

helm upgrade panvex deploy/helm/panvex \
  --reuse-values \
  --set bootstrap.enabled=true \
  --set bootstrap.passwordExistingSecret=panvex-bootstrap
```

The `bootstrap-admin` subcommand refuses to plant a user on a non-empty
store, so the Job is safe to re-run; once it succeeds, set
`bootstrap.enabled=false` (or just leave it — Helm will recreate the Job
on every release but the binary will exit successfully without changes).

## Audit-related notes

- **S-04 (audit-events vs SIGKILL):** `terminationGracePeriodSeconds` is set
  to 60s by default — exactly the buffer above
  `controlPlaneShutdownGraceMin = 45s` so the in-flight audit-event drain
  finishes before SIGKILL. Do NOT lower this without auditing the
  audit-event flush path.
- **S-06 (trusted proxies):** `trustedProxyCIDRs` defaults to a list that
  covers Kubernetes service CIDRs (`10.0.0.0/8`), Docker bridge
  (`172.16.0.0/12`), and IPv6 ULA (`fd00::/8`). Override per cluster.
- **S-09 (HSTS preload):** set `hstsPreload=1` to opt into the 2-year HSTS
  preload directive. Default is the 1-year HSTS without preload.

## Lint

```bash
helm lint deploy/helm/panvex
helm template panvex deploy/helm/panvex --debug | head -100
```
