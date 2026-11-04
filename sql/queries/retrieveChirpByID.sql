-- name: RetrieveChirpByID :one
SELECT id, created_at, updated_at, body, user_id
FROM chirps
WHERE id = $1;