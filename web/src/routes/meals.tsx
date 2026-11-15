import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import { Button, ButtonPrimary, Input, List, ListRow, Notice, PageHeader, Section, Sheet, Textarea } from '@app/design';

import { $api, errorMessage, idempotencyKey, type Meal, type MealSlot, type RecipeSummary } from '../api/client';

const slots: readonly MealSlot[] = ['breakfast', 'lunch', 'dinner'];
const slotLabels: Record<MealSlot, string> = { breakfast: 'Breakfast', lunch: 'Lunch', dinner: 'Dinner' };
export const RECIPE_TITLE_MAX = 160;
export const MEAL_NOTE_MAX = 200;

/** Dates from the API are household days (YYYY-MM-DD); label them without a time zone shift. */
function dayLabel(date: string, opts: Intl.DateTimeFormatOptions = { weekday: 'long', month: 'short', day: 'numeric' }) {
  const [y, m, d] = date.split('-').map(Number);
  return new Date(Date.UTC(y ?? 2000, (m ?? 1) - 1, d ?? 1)).toLocaleDateString(undefined, { ...opts, timeZone: 'UTC' });
}

function shiftWeek(date: string, weeks: number) {
  const [y, m, d] = date.split('-').map(Number);
  const t = new Date(Date.UTC(y ?? 2000, (m ?? 1) - 1, (d ?? 1) + weeks * 7));
  return t.toISOString().slice(0, 10);
}

interface Editing {
  date: string;
  slot: MealSlot;
  meal: Meal | undefined;
}

// Manage: the week's meal plan, every day and slot, empty slots ready to fill.
export function MealsPage() {
  const queryClient = useQueryClient();
  const [week, setWeek] = useState<string | undefined>(undefined);
  const plan = $api.useQuery('get', '/meals', { params: { query: week ? { week } : {} } });
  const recipes = $api.useQuery('get', '/recipes', {});
  const [editing, setEditing] = useState<Editing | null>(null);
  const start = plan.data?.weekStart;

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title="Meals this week"
        description={start ? `Week of ${dayLabel(start)}.` : undefined}
        actions={
          start && (
            <>
              <Button onClick={() => { setWeek(shiftWeek(start, -1)); }}>Previous week</Button>
              <Button onClick={() => { setWeek(shiftWeek(start, 1)); }}>Next week</Button>
            </>
          )
        }
      />
      {plan.error && <Notice tone="error">{errorMessage(plan.error)}</Notice>}
      {plan.data?.days.map((day) => (
        <Section key={day.date} title={dayLabel(day.date)}>
          <List>
            {slots.map((slot) => {
              const meal = day.meals.find((m) => m.slot === slot);
              const label = `${slotLabels[slot]}, ${dayLabel(day.date)}`;
              return (
                <ListRow
                  key={slot}
                  stackOnNarrow
                  muted={meal === undefined}
                  title={meal?.title ?? 'Nothing planned'}
                  meta={[slotLabels[slot], meal?.note].filter(Boolean).join(' · ')}
                  trailing={
                    <Button variant="quiet" aria-label={meal ? `Change ${label}` : `Plan ${label}`} onClick={() => { setEditing({ date: day.date, slot, meal }); }}>
                      {meal ? 'Change' : 'Plan'}
                    </Button>
                  }
                />
              );
            })}
          </List>
        </Section>
      ))}
      {editing && (
        <PlanSheet
          key={`${editing.date}-${editing.slot}`}
          editing={editing}
          recipes={recipes.data?.recipes ?? []}
          onDone={() => {
            setEditing(null);
            void queryClient.invalidateQueries({ queryKey: ['get', '/meals'] });
          }}
        />
      )}
    </section>
  );
}

function PlanSheet({ editing, recipes, onDone }: { editing: Editing; recipes: RecipeSummary[]; onDone: () => void }) {
  const { date, slot, meal } = editing;
  const [text, setText] = useState(meal?.title ?? '');
  const [note, setNote] = useState(meal?.note ?? '');
  const set = $api.useMutation('put', '/meals/{date}/{slot}', { onSuccess: onDone });
  const clear = $api.useMutation('delete', '/meals/{date}/{slot}', { onSuccess: onDone });
  const path = { date, slot };

  const save = (e: SyntheticEvent) => {
    e.preventDefault();
    // A name that matches a recipe plans that recipe; anything else is a plain entry.
    const typed = text.trim();
    const match = recipes.find((r) => !r.archived && r.title.toLowerCase() === typed.toLowerCase());
    const trimmedNote = note.trim();
    set.mutate({
      params: { ...idempotencyKey(), path },
      body: {
        ...(match ? { recipeId: match.id } : { title: typed }),
        ...(trimmedNote ? { note: trimmedNote } : {}),
        expectedVersion: meal?.version ?? 0,
      },
    });
  };
  const error = set.error ?? clear.error;

  return (
    <Sheet
      open
      onClose={onDone}
      title={`Plan ${dayLabel(date, { weekday: 'long' })}’s ${slotLabels[slot].toLowerCase()}`}
      footer={
        <>
          {meal && (
            <Button
              variant="danger"
              disabled={clear.isPending}
              onClick={() => { clear.mutate({ params: { ...idempotencyKey(), path, query: { expectedVersion: meal.version } } }); }}
            >
              Clear
            </Button>
          )}
          <Button onClick={onDone}>Cancel</Button>
          <ButtonPrimary type="submit" form="plan-meal" disabled={set.isPending || text.trim() === ''}>
            Save
          </ButtonPrimary>
        </>
      }
    >
      <form id="plan-meal" onSubmit={save} className="grid gap-4">
        <Input
          label="Meal"
          hint="A recipe’s name, or anything else such as Leftovers."
          list="recipe-titles"
          value={text}
          onChange={(e) => { setText(e.target.value); }}
          maxLength={RECIPE_TITLE_MAX}
          required
        />
        <datalist id="recipe-titles">
          {recipes.filter((r) => !r.archived).map((r) => (
            <option key={r.id} value={r.title} />
          ))}
        </datalist>
        <Textarea
          label="Note (optional)"
          hint={`Up to ${String(MEAL_NOTE_MAX)} characters.`}
          maxLength={MEAL_NOTE_MAX}
          value={note}
          onChange={(e) => { setNote(e.target.value); }}
        />
        {error && <Notice tone="error">{errorMessage(error)}</Notice>}
      </form>
    </Sheet>
  );
}

