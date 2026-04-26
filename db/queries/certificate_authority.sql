-- R-Q-03: certificate_authority — singleton row that stores the
-- control-plane root CA (PEM + private-key PEM, optionally encrypted).

-- name: GetCertificateAuthority :one
SELECT ca_pem, private_key_pem, updated_at
FROM certificate_authority
WHERE scope = $1;

-- name: UpsertCertificateAuthority :exec
INSERT INTO certificate_authority (scope, ca_pem, private_key_pem, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (scope) DO UPDATE
SET ca_pem = EXCLUDED.ca_pem,
    private_key_pem = EXCLUDED.private_key_pem,
    updated_at = EXCLUDED.updated_at;
