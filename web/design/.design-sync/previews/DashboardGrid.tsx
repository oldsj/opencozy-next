// @dsCard group="Dashboard"
import { CloudSun, LayoutDashboard, TriangleAlert } from 'lucide-react';

import {
  AppFrame,
  Button,
  ComingLaterTile,
  DashboardGrid,
  DashboardItem,
  List,
  ListRow,
  ManageNav,
  ManageShell,
  Notice,
  PageHeader,
  Panel,
  Sheet,
  Tag,
  TopBar,
  Widget,
} from '../../src';

import { dashboardErrorText, descriptorFor, hallway, kitchen, registry, type DashboardView, type ViewWidget } from './fixtures-dashboards';
import { manageGroups } from './fixtures-manage';
import { ThreePeople } from './PeopleBoard';
import { CalendarToday, HomeDoors, HomeNow, MealsToday, TvsOnDisplay } from './Widget';

const noop = () => undefined;

/** Reading order for the stacked phone layout: by row, then column. */
function ordered(widgets: ViewWidget[]): ViewWidget[] {
  return [...widgets].sort((a, b) => a.y - b.y || a.x - b.x);
}

/** A widget the server couldn't validate: isolated, so the rest of the view still shows. */
function BrokenWidget({ widget, editing = false }: { widget: ViewWidget; editing?: boolean }) {
  return (
    <Widget title={widget.config.title ?? 'Widget unavailable'} icon={TriangleAlert}>
      <p className="m-0 text-muted-foreground">{editing ? `${dashboardErrorText.broken} (${widget.reason ?? 'Unknown problem.'})` : 'This part of the screen isn’t available right now.'}</p>
    </Widget>
  );
}

function LiveWidget({ widget }: { widget: ViewWidget }) {
  if (!widget.valid) return <BrokenWidget widget={widget} />;
  switch (widget.type) {
    case 'today':
      return <ThreePeople />;
    case 'calendar':
      return <CalendarToday />;
    case 'meals':
      return <MealsToday />;
    case 'home':
      return widget.config.title === undefined ? <HomeNow /> : <HomeDoors title={widget.config.title} />;
    case 'tvs':
      return <TvsOnDisplay />;
    default:
      return <ComingLaterTile title={descriptorFor(widget.type)?.title ?? 'Coming later'} icon={CloudSun} />;
  }
}

/**
 * A named view on a display: each widget at its saved place. A broken widget is a quiet card of its
 * own; everything else carries on.
 */
function ViewOnDisplay({ view }: { view: DashboardView }) {
  return (
    <AppFrame topBar={<TopBar time="4:12" date="Monday, September 14" householdName="Maple House" />}>
      <DashboardGrid aria-label={`${view.name} dashboard`}>
        {ordered(view.widgets).map((w) => (
          <DashboardItem key={w.id} x={w.x} y={w.y} w={w.w} h={w.h}>
            <LiveWidget widget={w} />
          </DashboardItem>
        ))}
      </DashboardGrid>
    </AppFrame>
  );
}

export function KitchenView() {
  return <ViewOnDisplay view={kitchen} />;
}

/** One widget is unknown to this server (an older or newer version saved it): only it is replaced. */
export function ViewWithBrokenWidget() {
  return <ViewOnDisplay view={hallway} />;
}

// ---------------------------------------------------------------------------------------------------
// The editor (Manage → Dashboard → Edit). Everything is a button: select a widget, then move or resize
// it a cell at a time. Keyboard, touch and pointer users get the same moves; there is no drag-only
// path. Each change is announced in the status line.

function EditorTile({ widget, selected }: { widget: ViewWidget; selected: boolean }) {
  const d = descriptorFor(widget.type);
  if (!widget.valid || !d) return <BrokenWidget widget={widget} editing />;
  return (
    <Widget
      title={widget.config.title ?? d.title}
      action={
        <Button variant={selected ? 'primary' : 'quiet'} aria-pressed={selected} aria-label={`Select ${widget.config.title ?? d.title}`}>
          {selected ? 'Selected' : 'Select'}
        </Button>
      }
      footer={`${String(widget.w)} wide × ${String(widget.h)} tall`}
    >
      <p className="m-0 text-muted-foreground">{d.description}</p>
      {!d.available && <Tag>Coming later</Tag>}
    </Widget>
  );
}

function Toolbar({ widget, moved = false }: { widget: ViewWidget; moved?: boolean }) {
  const d = descriptorFor(widget.type);
  const name = widget.config.title ?? d?.title ?? 'widget';
  const min = d?.minSize ?? { w: 1, h: 1 };
  const max = d?.maxSize ?? { w: 6, h: 6 };
  return (
    <Panel heading={name} description={`Column ${String(widget.x + 1)}, row ${String(widget.y + 1)} · ${String(widget.w)} wide × ${String(widget.h)} tall`}>
      <div className="grid gap-3">
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label={`Move ${name}`}>
          <span className="w-12 font-semibold text-muted-foreground" aria-hidden="true">
            Move
          </span>
          <Button disabled={widget.x === 0} aria-label={`Move ${name} left`}>
            Left
          </Button>
          <Button disabled={widget.x + widget.w >= 6} aria-label={`Move ${name} right`}>
            Right
          </Button>
          <Button disabled={widget.y === 0} aria-label={`Move ${name} up`}>
            Up
          </Button>
          <Button aria-label={`Move ${name} down`}>Down</Button>
        </div>
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label={`Resize ${name}`}>
          <span className="w-12 font-semibold text-muted-foreground" aria-hidden="true">
            Size
          </span>
          <Button disabled={widget.w <= min.w} aria-label={`Make ${name} narrower`}>
            Narrower
          </Button>
          <Button disabled={widget.w >= max.w || widget.x + widget.w >= 6} aria-label={`Make ${name} wider`}>
            Wider
          </Button>
          <Button disabled={widget.h <= min.h} aria-label={`Make ${name} shorter`}>
            Shorter
          </Button>
          <Button disabled={widget.h >= max.h} aria-label={`Make ${name} taller`}>
            Taller
          </Button>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="quiet">Settings</Button>
          <Button variant="danger" aria-label={`Remove ${name}`}>
            Remove
          </Button>
        </div>
        {/* Announces each change, e.g. "Calendar moved up. Column 5, row 1." Empty until one is made. */}
        <p className="m-0 text-muted-foreground" role="status">
          {moved ? `${name} moved up. Column ${String(widget.x + 1)}, row ${String(widget.y + 1)}.` : null}
        </p>
      </div>
    </Panel>
  );
}

function Editor({ view, selectedId, conflict = false, moved = false }: { view: DashboardView; selectedId?: string; conflict?: boolean; moved?: boolean }) {
  const selected = view.widgets.find((w) => w.id === selectedId);
  return (
    <ManageShell nav={<ManageNav groups={manageGroups('/manage/dashboard')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}>
      <PageHeader
        title={`Edit ${view.name}`}
        description={`${String(view.widgets.length)} of 24 widgets. Shown on ${view.displays.length > 0 ? view.displays.join(', ') : 'no display yet'}.`}
        actions={
          <>
            <Button>Add a widget</Button>
            <Button>Cancel</Button>
            <Button variant="primary">Save view</Button>
          </>
        }
      />
      <div className="grid gap-6">
        {conflict && <Notice tone="attention">{dashboardErrorText.version_conflict}</Notice>}
        {selected ? <Toolbar widget={selected} moved={moved} /> : <p className="m-0 text-muted-foreground">Select a widget to move, resize or remove it.</p>}
        <DashboardGrid editing aria-label={`${view.name} layout`}>
          {ordered(view.widgets).map((w) => (
            <DashboardItem key={w.id} x={w.x} y={w.y} w={w.w} h={w.h} selected={w.id === selectedId}>
              <EditorTile widget={w} selected={w.id === selectedId} />
            </DashboardItem>
          ))}
        </DashboardGrid>
      </div>
    </ManageShell>
  );
}

export function EditView() {
  return <Editor view={kitchen} />;
}

/**
 * The calendar is selected, just after moving it up. At the right edge and the top it can't go
 * further right or up, or grow wider; at its minimum width it can't get narrower. A move or resize
 * into another widget's cells pushes that widget down, so nothing ever overlaps.
 */
export function EditViewSelected() {
  return <Editor view={kitchen} selectedId="w2" moved />;
}

/** A broken widget in the editor says why, and can be removed. */
export function EditViewBroken() {
  return <Editor view={hallway} selectedId="w8" />;
}

/** Saving after someone else saved: their version is shown and nothing is overwritten. */
export function EditViewConflict() {
  return <Editor view={kitchen} conflict />;
}

/** Adding a widget: it goes in the first free spot at its default size. */
export function AddWidget() {
  return (
    <>
      <Editor view={kitchen} />
      <Sheet open onClose={noop} title="Add a widget" description="It goes in the first free spot; move it from there.">
        <List>
          {registry.map((d) => (
            <ListRow
              key={d.type}
              leading={<LayoutDashboard size={20} aria-hidden="true" />}
              title={d.title}
              meta={
                d.available ? (
                  d.description
                ) : (
                  <>
                    <Tag>Coming later</Tag> {d.description}
                  </>
                )
              }
              trailing={
                <Button aria-label={`Add ${d.title}`} disabled={kitchen.widgets.length >= 24}>
                  Add
                </Button>
              }
            />
          ))}
        </List>
      </Sheet>
    </>
  );
}
