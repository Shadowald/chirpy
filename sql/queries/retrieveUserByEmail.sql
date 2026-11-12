-- name: RetrieveUserByEmail :one
SELECT *
FROM users
WHERE email = $1;