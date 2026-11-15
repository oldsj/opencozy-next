-- +goose Up
-- Slice 14: recipes and the household's meal plan.

-- text_items_valid: a 1-D array of at most max_items items, each with 1 to max_len characters
-- that aren't all blank. An empty array is fine.
CREATE FUNCTION text_items_valid(items text[], max_items integer, max_len integer) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
    items IS NOT NULL
    AND (cardinality(items) = 0 OR array_ndims(items) = 1)
    AND cardinality(items) <= max_items
    AND array_position(items, NULL) IS NULL
    AND NOT EXISTS (SELECT FROM unnest(items) AS i WHERE length(btrim(i)) NOT BETWEEN 1 AND max_len);

-- Recipes are archived, never deleted, so planned meals keep their recipe.
CREATE TABLE recipe (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    title        text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 160),
    servings     integer NOT NULL CHECK (servings BETWEEN 1 AND 100),
    ingredients  text[] NOT NULL DEFAULT '{}' CHECK (text_items_valid(ingredients, 40, 200)),
    steps        text[] NOT NULL DEFAULT '{}' CHECK (text_items_valid(steps, 40, 2000)),
    version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at   timestamptz NOT NULL DEFAULT now(),
    archived_at  timestamptz,
    UNIQUE (household_id, id)
);
CREATE INDEX recipe_household_title_idx ON recipe (household_id, lower(title));

-- One meal per slot per household day. A meal is a recipe, or a plain title such as "Leftovers".
CREATE TABLE meal (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    day          date NOT NULL CHECK (isfinite(day)),
    slot         text NOT NULL CHECK (slot IN ('breakfast', 'lunch', 'dinner')),
    recipe_id    uuid,
    title        text CHECK (length(btrim(title)) BETWEEN 1 AND 160),
    note         text CHECK (length(btrim(note)) BETWEEN 1 AND 200),
    version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, day, slot),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipe (household_id, id),
    CHECK ((recipe_id IS NULL) = (title IS NOT NULL))
);
CREATE INDEX meal_recipe_idx ON meal (household_id, recipe_id) WHERE recipe_id IS NOT NULL;

-- +goose Down
DROP TABLE meal;
DROP TABLE recipe;
DROP FUNCTION text_items_valid(text[], integer, integer);
