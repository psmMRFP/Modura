-- name: ListFeedbackCategories :many
SELECT * FROM wheretolive.feedback_categories WHERE active ORDER BY position,key;

-- name: ListFeedbackIntake :many
SELECT * FROM wheretolive.feedback_intake
WHERE (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
 AND (sqlc.arg(category_key)::text = '' OR category_key = sqlc.arg(category_key))
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: LockFeedbackIntake :one
SELECT * FROM wheretolive.feedback_intake WHERE id=$1 FOR UPDATE;

-- name: InsertFeedbackIntake :one
INSERT INTO wheretolive.feedback_intake (id,category_key,place_id,title,message,created_at,updated_at)
SELECT $1,c.key,$3,$4,$5,$6,$6 FROM wheretolive.feedback_categories c WHERE c.key=$2 AND c.active
RETURNING *;

-- name: ReviewFeedbackIntake :one
UPDATE wheretolive.feedback_intake SET status=$2,outcome=$3,updated_at=$4,version=version+1
WHERE id=$1 AND version=$5 RETURNING *;
