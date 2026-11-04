-- name: RetrieveChirps :many
SELECT id, created_at, updated_at, body, user_id
FROM chirps
ORDER BY created_at;