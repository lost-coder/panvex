-- R-Q-03: user_appearance — per-user theme/density/help-mode prefs.

-- name: GetUserAppearance :one
SELECT user_id, theme, density, help_mode, updated_at
FROM user_appearance
WHERE user_id = $1;

-- name: ListUserAppearances :many
SELECT user_id, theme, density, help_mode, updated_at
FROM user_appearance
ORDER BY user_id ASC;

-- name: UpsertUserAppearance :exec
INSERT INTO user_appearance (user_id, theme, density, help_mode, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET theme = EXCLUDED.theme,
    density = EXCLUDED.density,
    help_mode = EXCLUDED.help_mode,
    updated_at = EXCLUDED.updated_at;
