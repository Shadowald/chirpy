-- name: UpdateUser :exec
UPDATE users
SET hashed_password = $2,
    email = $3,
    updated_at = NOW()
WHERE id = $1;