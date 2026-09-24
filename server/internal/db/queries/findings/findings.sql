-- name: ListRunFindings :many
SELECT * FROM findings WHERE run_id = $1 ORDER BY id;
