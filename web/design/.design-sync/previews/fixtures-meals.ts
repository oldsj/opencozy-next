/**
 * Slice 14, typed to the shapes agreed with opencozy-pilot (fictional data). Dates are in household
 * time. A day has at most one meal per slot, so 0-3 meals.
 */
export type MealSlot = 'breakfast' | 'lunch' | 'dinner';

export const mealSlots: readonly MealSlot[] = ['breakfast', 'lunch', 'dinner'];

export const slotLabels: Record<MealSlot, string> = { breakfast: 'Breakfast', lunch: 'Lunch', dinner: 'Dinner' };

export interface Meal {
  id: string;
  slot: MealSlot;
  /** The recipe's title when recipeId is set; otherwise free text such as "Leftovers". */
  title: string;
  recipeId?: string | undefined;
  /** Up to 200 characters. */
  note?: string | undefined;
  /** For the conflict check; never shown. */
  version: number;
}

export interface MealDay {
  date: string;
  meals: Meal[];
}

/** GET /api/meals?week=<any date>: the household's week containing that date, 7 days. */
export interface MealWeek {
  weekStart: string;
  days: MealDay[];
}

/** GET /api/recipes. */
export interface RecipeSummary {
  id: string;
  /** Up to 160 characters; design for 80 visible and wrap the rest. */
  title: string;
  servings: number;
  /** Archived recipes stay on old meals but leave the suggestions. */
  archived: boolean;
}

/** GET /api/recipes/{id}. `version` is for the editor's conflict check and is never shown. */
export interface Recipe extends RecipeSummary {
  /** Up to 40, each up to 200 characters. Key rows by index. */
  ingredients: string[];
  /** Up to 40, each up to 2000 characters, shown wrapped in full. */
  steps: string[];
  /** For the editor's conflict check; never shown. */
  version: number;
}

/**
 * Planning a slot: a recipe or a plain title, an optional note, and the slot's current version
 * (0 when it is empty).
 */
export type PlanSlotRequest = ({ recipeId: string } | { title: string }) & { note?: string | undefined; expectedVersion: number };

export const RECIPE_TITLE_MAX = 160;
export const MEAL_NOTE_MAX = 200;
export const INGREDIENT_MAX = 200;
export const STEP_MAX = 2000;

export const week: MealWeek = {
  weekStart: '2026-09-14',
  days: [
    {
      date: '2026-09-14',
      meals: [
        { id: 'm1', slot: 'breakfast', title: 'Porridge with apples', recipeId: 'r2', version: 1 },
        { id: 'm2', slot: 'dinner', title: 'Lentil soup with crusty bread', recipeId: 'r1', note: 'Make a double batch for Wednesday.', version: 1 },
      ],
    },
    { date: '2026-09-15', meals: [{ id: 'm3', slot: 'dinner', title: 'Sheet-pan chicken and potatoes', recipeId: 'r3', version: 1 }] },
    { date: '2026-09-16', meals: [{ id: 'm4', slot: 'dinner', title: 'Leftovers', version: 1 }] },
    { date: '2026-09-17', meals: [] },
    {
      date: '2026-09-18',
      meals: [
        { id: 'm5', slot: 'lunch', title: 'Picnic sandwiches', note: 'School trip: pack two each.', version: 1 },
        { id: 'm6', slot: 'dinner', title: 'Homemade pizza night', recipeId: 'r4', version: 1 },
      ],
    },
    { date: '2026-09-19', meals: [{ id: 'm7', slot: 'breakfast', title: 'Pancakes', recipeId: 'r5', version: 1 }] },
    { date: '2026-09-20', meals: [] },
  ],
};

export const dayLabels: Record<string, string> = {
  '2026-09-14': 'Monday, Sep 14',
  '2026-09-15': 'Tuesday, Sep 15',
  '2026-09-16': 'Wednesday, Sep 16',
  '2026-09-17': 'Thursday, Sep 17',
  '2026-09-18': 'Friday, Sep 18',
  '2026-09-19': 'Saturday, Sep 19',
  '2026-09-20': 'Sunday, Sep 20',
};

export const today = '2026-09-14';

export const recipes: RecipeSummary[] = [
  { id: 'r1', title: 'Lentil soup with crusty bread', servings: 6, archived: false },
  { id: 'r2', title: 'Porridge with apples', servings: 4, archived: false },
  { id: 'r3', title: 'Sheet-pan chicken and potatoes with lemon, garlic and whatever greens are in the fridge', servings: 4, archived: false },
  { id: 'r4', title: 'Homemade pizza night', servings: 5, archived: false },
  { id: 'r5', title: 'Pancakes', servings: 4, archived: false },
  { id: 'r6', title: 'Summer gazpacho', servings: 4, archived: true },
];

export const recipe: Recipe = {
  id: 'r1',
  title: 'Lentil soup with crusty bread',
  servings: 6,
  archived: false,
  version: 3,
  ingredients: ['1 tbsp olive oil', '1 onion, chopped', '2 carrots, diced', '2 cloves garlic', '300 g red lentils', '1.5 l vegetable stock', '1 lemon', 'Crusty bread, to serve'],
  steps: [
    'Warm the oil in a large pot over a medium heat. Add the onion and carrots and cook gently for about ten minutes, stirring now and then, until soft but not brown.',
    'Add the garlic and cook for one more minute.',
    'Rinse the lentils, add them with the stock and bring to the boil. Turn the heat down and simmer for 20 to 25 minutes, until the lentils have fallen apart.\nTaste, then season with salt, pepper and a squeeze of lemon. Blend half of it if you like it thicker.',
    'Serve with warm bread.',
  ],
};
