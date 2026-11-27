import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useState, type SyntheticEvent } from 'react';
import { BookOpen } from 'lucide-react';

import { Button, ButtonPrimary, EmptyState, Input, List, ListRow, Notice, PageHeader, Section, Textarea } from '@app/design';

import { $api, errorMessage, idempotencyKey, type Recipe } from '../api/client';
import { RouterButtonLink } from '../link';
import { RECIPE_TITLE_MAX } from './meals';

const INGREDIENT_MAX = 200;
const STEP_MAX = 2000;
const ITEMS_MAX = 40;

// Manage: the household's recipes, by title.
export function RecipesPage() {
  const [archived, setArchived] = useState(false);
  const recipes = $api.useQuery('get', '/recipes', { params: { query: archived ? { archived: true } : {} } });
  const list = recipes.data?.recipes ?? [];
  return (
    <>
      <PageHeader
        title="Recipes"
        description="What the family cooks."
        actions={
          <>
            <RouterButtonLink to="/recipes/$recipeId" params={{ recipeId: 'new' }} variant="primary">Add recipe</RouterButtonLink>
            <Button variant="quiet" onClick={() => { setArchived(!archived); }}>
              {archived ? 'Hide archived' : 'Show archived'}
            </Button>
          </>
        }
      />
      {recipes.error && <Notice tone="error">{errorMessage(recipes.error)}</Notice>}
      {list.length ? (
        <Section title="All recipes" count={list.length}>
          <List>
            {list.map((r) => (
              <ListRow
                key={r.id}
                stackOnNarrow
                muted={r.archived}
                title={r.title}
                meta={[`Serves ${String(r.servings)}`, r.archived && 'Archived'].filter(Boolean).join(' · ')}
                trailing={
                  <Link to="/recipes/$recipeId" params={{ recipeId: r.id }} aria-label={`Open ${r.title}`}>
                    Open
                  </Link>
                }
              />
            ))}
          </List>
        </Section>
      ) : (
        recipes.isSuccess && <EmptyState variant="surface" icon={BookOpen} title="No recipes yet" description="Add the family’s favourites to plan meals with them." />
      )}
    </>
  );
}

// One recipe: the cook-from view, or the editor ("new" adds one).
export function RecipePage({ recipeId }: { recipeId: string }) {
  const isNew = recipeId === 'new';
  const recipe = $api.useQuery('get', '/recipes/{recipeId}', { params: { path: { recipeId } } }, { enabled: !isNew });
  const [editing, setEditing] = useState(false);

  if (isNew) return <RecipeForm />;
  if (recipe.error) return <Notice tone="error">{errorMessage(recipe.error)}</Notice>;
  if (!recipe.data) return null;
  const r = recipe.data;
  if (editing) return <RecipeForm recipe={r} onDone={() => { setEditing(false); }} />;
  return (
    <>
      <PageHeader
        title={r.title}
        description={[`Serves ${String(r.servings)}`, r.archived && 'Archived'].filter(Boolean).join(' · ')}
        actions={
          <>
            <RouterButtonLink to="/recipes" variant="quiet">All recipes</RouterButtonLink>
            <Button onClick={() => { setEditing(true); }}>Edit</Button>
          </>
        }
      />
      <Section title="Ingredients" count={r.ingredients.length}>
        <ul className="grid gap-2">
          {r.ingredients.map((x, i) => (
            <li key={i}>{x}</li>
          ))}
        </ul>
      </Section>
      <Section title="Steps" count={r.steps.length}>
        <ol className="grid list-decimal gap-4 ps-6">
          {r.steps.map((x, i) => (
            <li key={i} className="whitespace-pre-line">{x}</li>
          ))}
        </ol>
      </Section>
    </>
  );
}

function RecipeForm({ recipe, onDone }: { recipe?: Recipe; onDone?: () => void }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [title, setTitle] = useState(recipe?.title ?? '');
  const [servings, setServings] = useState(String(recipe?.servings ?? 4));
  const [ingredients, setIngredients] = useState((recipe?.ingredients ?? []).join('\n'));
  const [steps, setSteps] = useState<string[]>(recipe?.steps.length ? recipe.steps : ['']);
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/recipes'] });
  const create = $api.useMutation('post', '/recipes', {
    onSuccess: (r) => { void refresh(); void navigate({ to: '/recipes/$recipeId', params: { recipeId: r.id } }); },
  });
  const update = $api.useMutation('put', '/recipes/{recipeId}', {
    onSuccess: (r) => {
      queryClient.setQueryData(['get', '/recipes/{recipeId}', { params: { path: { recipeId: r.id } } }], r);
      void refresh();
      void queryClient.invalidateQueries({ queryKey: ['get', '/meals'] });
      onDone?.();
    },
  });

  // Ingredients are one per line; blank lines and blank steps are dropped.
  const body = () => ({
    title: title.trim(),
    servings: Number(servings),
    ingredients: ingredients.split('\n').map((x) => x.trim()).filter(Boolean),
    steps: steps.map((x) => x.trim()).filter(Boolean),
  });
  const save = (archived: boolean) => (e?: SyntheticEvent) => {
    e?.preventDefault();
    if (recipe) {
      update.mutate({ params: { ...idempotencyKey(), path: { recipeId: recipe.id } }, body: { ...body(), archived, expectedVersion: recipe.version } });
    } else {
      create.mutate({ params: idempotencyKey(), body: body() });
    }
  };
  const lines = ingredients.split('\n').filter((x) => x.trim()).length;
  const error = create.error ?? update.error;
  const pending = create.isPending || update.isPending;

  return (
    <>
      <PageHeader title={recipe ? 'Edit recipe' : 'Add recipe'} />
      <form onSubmit={save(recipe?.archived ?? false)} className="grid gap-6" aria-label={recipe ? 'Edit recipe' : 'Add recipe'}>
        <Input label="Name" value={title} onChange={(e) => { setTitle(e.target.value); }} maxLength={RECIPE_TITLE_MAX} required />
        <Input label="Serves" type="number" inputMode="numeric" min={1} max={100} value={servings} onChange={(e) => { setServings(e.target.value); }} required />
        <Textarea
          label="Ingredients"
          hint={`One per line, up to ${String(ITEMS_MAX)}, each up to ${String(INGREDIENT_MAX)} characters.`}
          rows={8}
          value={ingredients}
          onChange={(e) => { setIngredients(e.target.value); }}
        />
        {lines > ITEMS_MAX && <Notice tone="error">{`That's ${String(lines)} ingredients; the most is ${String(ITEMS_MAX)}.`}</Notice>}
        <Section title="Steps" description={`Up to ${String(ITEMS_MAX)}. Each step can have several lines.`}>
          <ol className="grid gap-4">
            {steps.map((step, i) => (
              // Steps have no ids: key by position, as the API returns them.
              <li key={i} className="grid gap-2">
                <Textarea
                  label={`Step ${String(i + 1)}`}
                  maxLength={STEP_MAX}
                  rows={3}
                  value={step}
                  onChange={(e) => { setSteps(steps.map((s, j) => (j === i ? e.target.value : s))); }}
                />
                <div>
                  <Button variant="quiet" aria-label={`Remove step ${String(i + 1)}`} onClick={() => { setSteps(steps.filter((_, j) => j !== i)); }}>
                    Remove
                  </Button>
                </div>
              </li>
            ))}
          </ol>
        </Section>
        <div className="flex flex-wrap gap-3">
          <Button onClick={() => { setSteps([...steps, '']); }} disabled={steps.length >= ITEMS_MAX}>Add a step</Button>
        </div>
        {error && <Notice tone="error">{errorMessage(error)}</Notice>}
        <div className="flex flex-wrap gap-3">
          <ButtonPrimary type="submit" disabled={pending || title.trim() === '' || lines > ITEMS_MAX}>Save recipe</ButtonPrimary>
          {recipe && (
            <Button variant={recipe.archived ? 'secondary' : 'danger'} onClick={save(!recipe.archived)} disabled={pending}>
              {recipe.archived ? 'Restore' : 'Archive'}
            </Button>
          )}
          {recipe ? <Button onClick={onDone}>Cancel</Button> : <RouterButtonLink to="/recipes">Cancel</RouterButtonLink>}
        </div>
      </form>
    </>
  );
}
