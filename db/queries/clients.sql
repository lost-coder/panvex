-- R-Q-03: clients — managed-client core record. The `clients` group of
-- tables (assignments, deployments, usage, ip_history) gets one query
-- file per table; this file owns the parent.

-- name: GetClient :one
SELECT id, name, secret_ciphertext, user_ad_tag, enabled, max_tcp_conns,
       max_unique_ips, data_quota_bytes, expiration_rfc3339,
       created_at, updated_at, deleted_at
FROM clients
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListClients :many
SELECT id, name, secret_ciphertext, user_ad_tag, enabled, max_tcp_conns,
       max_unique_ips, data_quota_bytes, expiration_rfc3339,
       created_at, updated_at, deleted_at
FROM clients
WHERE deleted_at IS NULL
ORDER BY created_at ASC, id ASC;


-- name: UpsertClient :exec
INSERT INTO clients (id, name, secret_ciphertext, user_ad_tag, enabled,
                     max_tcp_conns, max_unique_ips, data_quota_bytes,
                     expiration_rfc3339, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE
SET name              = EXCLUDED.name,
    secret_ciphertext = EXCLUDED.secret_ciphertext,
    user_ad_tag       = EXCLUDED.user_ad_tag,
    enabled           = EXCLUDED.enabled,
    max_tcp_conns     = EXCLUDED.max_tcp_conns,
    max_unique_ips    = EXCLUDED.max_unique_ips,
    data_quota_bytes  = EXCLUDED.data_quota_bytes,
    expiration_rfc3339 = EXCLUDED.expiration_rfc3339,
    updated_at        = EXCLUDED.updated_at,
    deleted_at        = NULL;

-- name: SoftDeleteClient :execrows
UPDATE clients SET deleted_at = $2, updated_at = $2 WHERE id = $1 AND deleted_at IS NULL;
