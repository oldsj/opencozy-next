package importer

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// importRecipes carries recipes with their IDs, versions and archive times. The old limits
// (title 160, servings 1-100, up to 40 ingredients of 200 and 40 steps of 2000) are the new ones.
func importRecipes(ctx context.Context, x *run) error {
	x.r.columnsRead("recipe", "household_id", "id", "title", "servings", "ingredients", "instructions", "version", "created_at", "archived_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, title, servings, ingredients, instructions, version, created_at, archived_at
		FROM opencozy.recipe ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	var n int64
	for rows.Next() {
		var (
			household, id      uuid.UUID
			title              string
			servings, version  int32
			ingredients, steps []string
			created            time.Time
			archived           *time.Time
		)
		if err := rows.Scan(&household, &id, &title, &servings, &ingredients, &steps, &version, &created, &archived); err != nil {
			return safe(err)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO recipe (household_id, id, title, servings, ingredients, steps, version, created_at, archived_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, household, id, title, servings, ingredients, steps, version, created, archived); err != nil {
			return safe(err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	return reconcile(ctx, x, "recipe", n, "recipe")
}

// importMeals carries the meal plan: every old entry is a recipe in a breakfast, lunch or dinner
// slot, with no note.
func importMeals(ctx context.Context, x *run) error {
	x.r.columnsRead("meal_plan_entry", "household_id", "id", "local_day", "meal_type", "recipe_id", "version", "created_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, local_day, meal_type, recipe_id, version, created_at
		FROM opencozy.meal_plan_entry ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	var n int64
	for rows.Next() {
		var (
			household, id, recipe uuid.UUID
			day                   pgtype.Date
			slot                  string
			version               int32
			created               time.Time
		)
		if err := rows.Scan(&household, &id, &day, &slot, &recipe, &version, &created); err != nil {
			return safe(err)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO meal (household_id, id, day, slot, recipe_id, version, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, household, id, day, slot, recipe, version, created); err != nil {
			return safe(err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	return reconcile(ctx, x, "meal_plan_entry", n, "meal")
}
