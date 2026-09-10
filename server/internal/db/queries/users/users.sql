-- name: CreateUser :one
INSERT INTO users (email, name, google_subject, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByGoogleSubject :one
SELECT * FROM users WHERE google_subject = @google_subject::text;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower(@email::text);

-- name: BindGoogleSubject :one
UPDATE users SET google_subject = sqlc.arg(google_subject)::text
WHERE id = $1
RETURNING *;
