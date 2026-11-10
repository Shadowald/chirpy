-- name: GetUserFromRefreshToken :one
SELECT *
FROM users
Join refresh_tokens ON users.id = refresh_tokens.user_id
WHERE refresh_tokens.token = $1;