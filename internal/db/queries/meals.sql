-- name: ListRecipes :many
SELECT * FROM recipe
WHERE household_id = $1 AND (sqlc.arg(include_archived)::boolean OR archived_at IS NULL)
ORDER BY lower(title), id
LIMIT 1000;

-- name: CountRecipes :one
SELECT count(*) FROM recipe WHERE household_id = $1 AND archived_at IS NULL;

-- name: GetRecipe :one
SELECT * FROM recipe WHERE household_id = $1 AND id = $2;

-- name: CreateRecipe :one
INSERT INTO recipe (household_id, title, servings, ingredients, steps, created_at)
VALUES ($1, $2, $3, $4, $5, @now)
RETURNING *;

-- name: UpdateRecipe :one
UPDATE recipe
SET title = $4, servings = $5, ingredients = $6, steps = $7,
    archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN coalesce(archived_at, sqlc.arg(now)::timestamptz) ELSE NULL END,
    version = version + 1
WHERE household_id = $1 AND id = $2 AND version = $3
RETURNING *;

-- name: LockRecipe :one
SELECT id, title FROM recipe WHERE household_id = $1 AND id = $2 FOR SHARE;

-- name: ListMeals :many
SELECT m.id, m.day, m.slot, m.recipe_id, coalesce(m.title, r.title)::text AS title, m.note, m.version
FROM meal m LEFT JOIN recipe r ON r.household_id = m.household_id AND r.id = m.recipe_id
WHERE m.household_id = $1 AND m.day BETWEEN @first::date AND @last::date
ORDER BY m.day, m.slot;

-- name: LockMeal :one
SELECT id, version FROM meal WHERE household_id = $1 AND day = $2 AND slot = $3 FOR UPDATE;

-- name: InsertMeal :one
INSERT INTO meal (household_id, day, slot, recipe_id, title, note, created_at)
VALUES ($1, $2, $3, $4, $5, $6, @now)
ON CONFLICT (household_id, day, slot) DO NOTHING
RETURNING id, version;

-- name: ReplaceMeal :one
UPDATE meal SET recipe_id = $3, title = $4, note = $5, version = version + 1
WHERE household_id = $1 AND id = $2
RETURNING id, version;

-- name: DeleteMeal :exec
DELETE FROM meal WHERE household_id = $1 AND id = $2;

