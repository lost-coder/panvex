-- R-Q-03: panel_settings — operator-tunable HTTP/gRPC public URLs and security policy.

-- name: GetPanelSettings :one
SELECT http_public_url, grpc_public_endpoint, password_min_length, updated_at
FROM panel_settings
WHERE scope = $1;

-- name: UpsertPanelSettings :exec
INSERT INTO panel_settings (scope, http_public_url, grpc_public_endpoint, password_min_length, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (scope) DO UPDATE
SET http_public_url = EXCLUDED.http_public_url,
    grpc_public_endpoint = EXCLUDED.grpc_public_endpoint,
    password_min_length = EXCLUDED.password_min_length,
    updated_at = EXCLUDED.updated_at;
