// @dsCard group="Layout"
import { CalendarDays, Gift, House, Settings, Sunrise } from 'lucide-react';

import { AppFrame, Button, PageHeader, PersonCard, Section, StatusPill, TabBar, TaskGroup, TaskRow, TopBar } from '../../src';

import { recipe } from './fixtures-meals';

export function Default() {
  return (
    <AppFrame
      topBar={
        <TopBar
          time="10:53"
          date="Sunday, September 13"
          householdName="Maple House"
          status={<StatusPill status="reconnecting" />}
          actions={
            <Button aria-label="Open family settings">
              <Settings aria-hidden="true" size={22} />
            </Button>
          }
        />
      }
      tabBar={
        <TabBar
          items={[
            { href: '#today', label: 'Today', icon: House, current: true },
            { href: '#rewards', label: 'Rewards', icon: Gift },
            { href: '#calendar', label: 'Calendar', icon: CalendarDays },
          ]}
        />
      }
    >
      <p>Family route content fills this working area.</p>
    </AppFrame>
  );
}

/** One child's page (/kid): the page takes their colour. */
export function Kid() {
  return (
    <AppFrame person="sky" topBar={<TopBar time="7:42" date="Monday, September 14" householdName="Maple House" />}>
      <PersonCard name="Abby" color="sky" points={24} completed={1} total={2}>
        <TaskGroup title="Morning" icon={Sunrise} status="3 points">
          <TaskRow title="Make the bed" check={{ status: 'complete', checked: true, label: 'Make the bed' }} />
          <TaskRow title="Pack school bag" check={{ status: 'open', checked: false, label: 'Pack school bag' }} />
        </TaskGroup>
      </PersonCard>
    </AppFrame>
  );
}

/** A recipe to cook from (family route, kiosk-sized): ingredients, then every step in full. */
export function RecipeView() {
  return (
    <AppFrame topBar={<TopBar time="5:32" date="Monday, September 14" householdName="Maple House" />}>
      <PageHeader title={recipe.title} description={`Serves ${String(recipe.servings)}`} />
      <div className="grid gap-8 md:grid-cols-[minmax(14rem,1fr)_2fr]">
        <Section title="Ingredients">
          <ul className="grid list-disc gap-2 pl-6">
            {recipe.ingredients.map((item, i) => (
              <li key={i}>{item}</li>
            ))}
          </ul>
        </Section>
        <Section title="Steps">
          <ol className="grid list-decimal gap-4 pl-6">
            {recipe.steps.map((step, i) => (
              <li key={i} className="whitespace-pre-line">
                {step}
              </li>
            ))}
          </ol>
        </Section>
      </div>
    </AppFrame>
  );
}
