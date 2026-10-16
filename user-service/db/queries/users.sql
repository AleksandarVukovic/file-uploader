-- name: GetByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: CreateUser :one
INSERT INTO users (username, email, first_name, last_name, password_hash)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
