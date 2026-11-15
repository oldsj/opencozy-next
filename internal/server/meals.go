package server

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
)

// maxRecipes caps a household's live (unarchived) recipes; the list returns at most 1000.
const maxRecipes = 1000

var slotOrder = []api.MealSlot{api.Breakfast, api.Lunch, api.Dinner}

// planYears bounds the dates a meal can be planned on, to keep typos like 0226 out.
const firstPlanYear, lastPlanYear = 2000, 2199

func apiDate(d day.Date) openapi_types.Date {
	return openapi_types.Date{Time: time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)}
}

func dayOf(d openapi_types.Date) day.Date {
	return day.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()}
}

func plannable(d day.Date) bool { return d.Year >= firstPlanYear && d.Year <= lastPlanYear }

// weekStart is the Monday on or before d.
func weekStart(d day.Date) day.Date {
	return d.AddDays(-((int(d.Weekday()) + 6) % 7))
}

func householdToday(ctx context.Context, q *dbq.Queries, householdID uuid.UUID) (day.Date, error) {
	h, err := q.GetHousehold(ctx, householdID)
	if err != nil {
		return day.Date{}, err
	}
	loc, err := day.Zone(h.TimeZone)
	if err != nil {
		return day.Date{}, err
	}
	now, err := q.Now(ctx)
	if err != nil {
		return day.Date{}, err
	}
	return day.Of(now, loc), nil
}

// GetMealWeek returns the seven days of the household week containing the date (default today).
func (s *Server) GetMealWeek(ctx context.Context, req api.GetMealWeekRequestObject) (api.GetMealWeekResponseObject, error) {
	h := mustActor(ctx).HouseholdID
	var d day.Date
	if req.Params.Week != nil {
		d = dayOf(*req.Params.Week)
		if !plannable(d) {
			return api.GetMealWeek400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError("bad_date", "Choose a date between 2000 and 2199."))}, nil
		}
	} else {
		var err error
		if d, err = householdToday(ctx, s.q, h); err != nil {
			return nil, err
		}
	}
	first := weekStart(d)
	rows, err := s.q.ListMeals(ctx, dbq.ListMealsParams{HouseholdID: h, First: pgDate(first), Last: pgDate(first.AddDays(6))})
	if err != nil {
		return nil, err
	}
	out := api.MealWeek{WeekStart: apiDate(first), Days: make([]api.MealDay, 7)}
	for i := range out.Days {
		out.Days[i] = api.MealDay{Date: apiDate(first.AddDays(i)), Meals: []api.Meal{}}
	}
	for _, m := range rows {
		i := int(m.Day.Time.Sub(pgDate(first).Time).Hours() / 24)
		if i < 0 || i > 6 {
			continue
		}
		out.Days[i].Meals = append(out.Days[i].Meals, api.Meal{
			Id: m.ID, Slot: api.MealSlot(m.Slot), Title: m.Title, RecipeId: m.RecipeID, Note: m.Note, Version: int(m.Version),
		})
	}
	for i := range out.Days {
		slices.SortFunc(out.Days[i].Meals, func(a, b api.Meal) int {
			return slices.Index(slotOrder, a.Slot) - slices.Index(slotOrder, b.Slot)
		})
	}
	return api.GetMealWeek200JSONResponse(out), nil
}

func conflict(what string) api.ConflictJSONResponse {
	return api.ConflictJSONResponse(apiError("version_conflict", "Someone changed this "+what+" meanwhile. Reload and try again."))
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// SetMeal plans a meal in a slot, or replaces the planned one at the expected version.
func (s *Server) SetMeal(ctx context.Context, req api.SetMealRequestObject) (api.SetMealResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	b := req.Body
	d := dayOf(req.Date)
	if !plannable(d) {
		return api.SetMeal400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError("bad_date", "Choose a date between 2000 and 2199."))}, nil
	}
	if (b.RecipeId == nil) == (b.Title == nil) {
		return api.SetMeal422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("recipe_or_title", "Choose a recipe or type what's planned, not both."))}, nil
	}
	title := trimmed(b.Title)
	if b.RecipeId != nil {
		r, err := cmd.Q.LockRecipe(ctx, dbq.LockRecipeParams{HouseholdID: h, ID: *b.RecipeId})
		if errors.Is(err, pgx.ErrNoRows) {
			return api.SetMeal404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(apiError("no_such_recipe", "That recipe isn't in this household."))}, nil
		} else if err != nil {
			return nil, err
		}
		title = &r.Title
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	stored := trimmed(b.Title) // NULL for a recipe: the meal shows the recipe's current title
	cur, err := cmd.Q.LockMeal(ctx, dbq.LockMealParams{HouseholdID: h, Day: pgDate(d), Slot: string(req.Slot)})
	var row dbq.InsertMealRow
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if b.ExpectedVersion != 0 {
			return api.SetMeal409JSONResponse{ConflictJSONResponse: conflict("meal")}, nil
		}
		row, err = cmd.Q.InsertMeal(ctx, dbq.InsertMealParams{HouseholdID: h, Day: pgDate(d), Slot: string(req.Slot), RecipeID: b.RecipeId, Title: stored, Note: trimmed(b.Note), Now: now})
		if errors.Is(err, pgx.ErrNoRows) { // planned meanwhile (the household lock makes this rare)
			return api.SetMeal409JSONResponse{ConflictJSONResponse: conflict("meal")}, nil
		}
	case err != nil:
		return nil, err
	default:
		if int(cur.Version) != b.ExpectedVersion {
			return api.SetMeal409JSONResponse{ConflictJSONResponse: conflict("meal")}, nil
		}
		var r dbq.ReplaceMealRow
		r, err = cmd.Q.ReplaceMeal(ctx, dbq.ReplaceMealParams{HouseholdID: h, ID: cur.ID, RecipeID: b.RecipeId, Title: stored, Note: trimmed(b.Note)})
		row = dbq.InsertMealRow(r)
	}
	if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Meals}); err != nil {
		return nil, err
	}
	return api.SetMeal200JSONResponse{Id: row.ID, Slot: req.Slot, Title: *title, RecipeId: b.RecipeId, Note: trimmed(b.Note), Version: int(row.Version)}, nil
}

// ClearMeal removes the planned meal at the expected version.
func (s *Server) ClearMeal(ctx context.Context, req api.ClearMealRequestObject) (api.ClearMealResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	d := dayOf(req.Date)
	if !plannable(d) {
		return api.ClearMeal404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	cur, err := cmd.Q.LockMeal(ctx, dbq.LockMealParams{HouseholdID: h, Day: pgDate(d), Slot: string(req.Slot)})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.ClearMeal404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	if int(cur.Version) != req.Params.ExpectedVersion {
		return api.ClearMeal409JSONResponse{ConflictJSONResponse: conflict("meal")}, nil
	}
	if err := cmd.Q.DeleteMeal(ctx, dbq.DeleteMealParams{HouseholdID: h, ID: cur.ID}); err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Meals}); err != nil {
		return nil, err
	}
	return api.ClearMeal204Response{}, nil
}

func toRecipe(r dbq.Recipe) api.Recipe {
	return api.Recipe{
		Id: r.ID, Title: r.Title, Servings: int(r.Servings), Archived: r.ArchivedAt != nil,
		Ingredients: r.Ingredients, Steps: r.Steps, Version: int(r.Version),
	}
}

func trimAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

// ListRecipes lists the household's recipes by title.
func (s *Server) ListRecipes(ctx context.Context, req api.ListRecipesRequestObject) (api.ListRecipesResponseObject, error) {
	rows, err := s.q.ListRecipes(ctx, dbq.ListRecipesParams{HouseholdID: mustActor(ctx).HouseholdID, IncludeArchived: req.Params.Archived != nil && *req.Params.Archived})
	if err != nil {
		return nil, err
	}
	out := api.RecipeList{Recipes: make([]api.RecipeSummary, len(rows))}
	for i, r := range rows {
		out.Recipes[i] = api.RecipeSummary{Id: r.ID, Title: r.Title, Servings: int(r.Servings), Archived: r.ArchivedAt != nil}
	}
	return api.ListRecipes200JSONResponse(out), nil
}

// GetRecipe returns one recipe.
func (s *Server) GetRecipe(ctx context.Context, req api.GetRecipeRequestObject) (api.GetRecipeResponseObject, error) {
	r, err := s.q.GetRecipe(ctx, dbq.GetRecipeParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.RecipeId})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetRecipe404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	return api.GetRecipe200JSONResponse(toRecipe(r)), nil
}

// CreateRecipe adds a recipe.
func (s *Server) CreateRecipe(ctx context.Context, req api.CreateRecipeRequestObject) (api.CreateRecipeResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	n, err := cmd.Q.CountRecipes(ctx, h)
	if err != nil {
		return nil, err
	}
	if n >= maxRecipes {
		return api.CreateRecipe422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("too_many_recipes", "This household has 1000 recipes. Archive some to add more."))}, nil
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	r, err := cmd.Q.CreateRecipe(ctx, dbq.CreateRecipeParams{
		HouseholdID: h, Title: strings.TrimSpace(b.Title), Servings: int32(b.Servings), //nolint:gosec // the spec caps it at 100
		Ingredients: trimAll(b.Ingredients), Steps: trimAll(b.Steps), Now: now,
	})
	if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Meals}); err != nil {
		return nil, err
	}
	return api.CreateRecipe201JSONResponse(toRecipe(r)), nil
}

// UpdateRecipe edits, archives or restores a recipe at the expected version.
func (s *Server) UpdateRecipe(ctx context.Context, req api.UpdateRecipeRequestObject) (api.UpdateRecipeResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	b := req.Body
	if !b.Archived {
		cur, err := cmd.Q.GetRecipe(ctx, dbq.GetRecipeParams{HouseholdID: h, ID: req.RecipeId})
		if err == nil && cur.ArchivedAt != nil { // restoring counts against the cap
			n, err := cmd.Q.CountRecipes(ctx, h)
			if err != nil {
				return nil, err
			}
			if n >= maxRecipes {
				return api.UpdateRecipe422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("too_many_recipes", "This household has 1000 recipes. Archive some to restore this one."))}, nil
			}
		}
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	r, err := cmd.Q.UpdateRecipe(ctx, dbq.UpdateRecipeParams{
		HouseholdID: h, ID: req.RecipeId, Version: int32(b.ExpectedVersion), //nolint:gosec // the spec caps it at int32
		Title: strings.TrimSpace(b.Title), Servings: int32(b.Servings), //nolint:gosec // the spec caps it at 100
		Ingredients: trimAll(b.Ingredients), Steps: trimAll(b.Steps), Archived: b.Archived, Now: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err := cmd.Q.GetRecipe(ctx, dbq.GetRecipeParams{HouseholdID: h, ID: req.RecipeId})
		if errors.Is(err, pgx.ErrNoRows) {
			return api.UpdateRecipe404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		} else if err != nil {
			return nil, err
		}
		return api.UpdateRecipe409JSONResponse{ConflictJSONResponse: conflict("recipe")}, nil
	} else if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Meals}); err != nil {
		return nil, err
	}
	return api.UpdateRecipe200JSONResponse(toRecipe(r)), nil
}
