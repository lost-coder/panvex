# Panvex network topology

Two listeners, two different security models:

| Port | Protocol | Security | Exposure |
|------|----------|----------|----------|
| 8080 | HTTP (panel UI/API) | None at this hop — TLS MUST terminate in front (nginx/ingress) | Loopback only; reverse proxy in front |
| 8443 | gRPC (agents) | mTLS end-to-end: server cert from the embedded CA, agent client certs, SPKI pinning both ways | Published directly (canonical) |

## Canonical: publish 8443 directly

`deploy/docker-compose.prod.yml` maps `8443:8443` on all interfaces.
The channel carries its own mTLS, so no proxy is required and none is
configured. Firewall posture: allow inbound `8443/tcp` from agent
networks; keep 8080 reachable only from the reverse proxy host.

Agents are pointed at the panel host directly:

```bash
panvex-agent bootstrap -mode reverse \
  --panel-url-grpc panel.example.com:8443 ...
```

## Alternative: nginx L4 stream passthrough (single public IP)

If policy requires every public port to terminate at the reverse-proxy
host, forward TCP without touching TLS (gRPC mTLS stays end-to-end —
do NOT terminate TLS here, the agent pins the panel CA):

```nginx
# /etc/nginx/nginx.conf — top level, OUTSIDE the http {} block.
stream {
  upstream panvex_grpc {
    server 127.0.0.1:8443;
  }
  server {
    listen 8443;
    proxy_pass panvex_grpc;
    proxy_connect_timeout 10s;
    # Long-lived bidirectional streams: keep well above the agent
    # heartbeat interval.
    proxy_timeout 1h;
  }
}
```

With this topology revert the compose mapping to
`"127.0.0.1:8443:8443"` so the only public entry point is nginx.

## Kubernetes (helm chart)

The chart's Service is `ClusterIP` — in-cluster agents reach
`<release>:8443` directly. Agents OUTSIDE the cluster need an L4 path:
set `service.type=LoadBalancer`, or add a TCP entry to your ingress
controller (e.g. ingress-nginx `--tcp-services-configmap`), or a
NodePort. Do not route gRPC through an HTTP(S) Ingress object — it
would terminate TLS and break the agent's CA pin.
