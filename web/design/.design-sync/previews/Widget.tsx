// @dsCard group="Dashboard"
import { CalendarDays, UtensilsCrossed } from 'lucide-react';

import { ButtonLink, EmptyState, List, ListRow, Widget } from '../../src';

import { mealSlots, slotLabels, today, week, type MealDay } from './fixtures-meals';

/** Today's meals, slot by slot; an empty slot says so instead of disappearing. */
export function MealsToday({ day = week.days.find((d) => d.date === today) }: { day?: MealDay | undefined }) {
  const meals = day?.meals ?? [];
  return (
    <Widget
      title="Meals"
      icon={UtensilsCrossed}
      action={
        <ButtonLink variant="quiet" href="#meals">
          This week
        </ButtonLink>
      }
    >
      {meals.length === 0 ? (
        <EmptyState title="Nothing planned today" description="Plan meals for the week from the adult pages." />
      ) : (
        <List>
          {mealSlots.map((slot) => {
            const meal = meals.find((m) => m.slot === slot);
            return (
              <ListRow
                key={slot}
                muted={meal === undefined}
                title={meal?.title ?? 'Nothing planned'}
                meta={[slotLabels[slot], meal?.note].filter(Boolean).join(' · ')}
              />
            );
          })}
        </List>
      )}
    </Widget>
  );
}

export function MealsEmptyDay() {
  return <MealsToday day={week.days.find((d) => d.date === '2026-09-17')} />;
}

/** The frame on its own, with a footer line. */
export function Frame() {
  return (
    <Widget title="Calendar" icon={CalendarDays} footer="Updated 7:40 AM">
      <p>A widget holds one glanceable block; the full view sits behind its action.</p>
    </Widget>
  );
}
