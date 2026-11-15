import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CalendarDays, Gift, House, Inbox, Sunrise, Users } from 'lucide-react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import {
  AppFrame,
  Button,
  ButtonLink,
  ChoiceGroup,
  CodeInput,
  ManageNav,
  ConnectionBanner,
  CheckCircle,
  ComingLaterTile,
  ConfirmDialog,
  Countdown,
  EmptyState,
  Field,
  Input,
  List,
  ListRow,
  Notice,
  PageHeader,
  PeopleBoard,
  PersonCard,
  PointsBadge,
  pointsLabel,
  useSurface,
  PersonTab,
  ProfileAvatar,
  QrCode,
  qrModulesFromRows,
  RewardTile,
  Section,
  SegmentedControl,
  Sheet,
  StatusPill,
  TabBar,
  TaskGroup,
  TaskRow,
  Textarea,
  Toast,
  TopBar,
  countdownLabel,
  countdownSpoken,
  profileColors,
} from '..';

beforeAll(() => {
  // jsdom has <dialog> but not its modal methods.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute('open');
  };
});

describe('foundations', () => {
  it('has eight stable profile keys', () => {
    expect(profileColors).toEqual(['rose', 'sage', 'sky', 'butter', 'lilac', 'peach', 'teal', 'sand']);
  });

  it('names an avatar by the person, not its colour', () => {
    render(<ProfileAvatar name="Ada" color="sky" />);
    expect(screen.getByRole('img', { name: 'Ada' })).toHaveTextContent('A');
  });
});

describe('frame and navigation', () => {
  it('has a skip link, one main landmark and the current page marked', () => {
    render(
      <AppFrame
        topBar={<TopBar time="10:53" date="Sunday, September 13" householdName="Maple House" />}
        tabBar={
          <TabBar
            items={[
              { href: '/today', label: 'Today', icon: House, current: true },
              { href: '/rewards', label: 'Rewards', icon: Gift },
            ]}
          />
        }
      >
        <p>Ready for today.</p>
      </AppFrame>,
    );
    expect(screen.getByRole('link', { name: 'Skip to content' })).toHaveAttribute('href', '#main-content');
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main-content');
    expect(screen.getByRole('link', { name: 'Today' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: 'Rewards' })).not.toHaveAttribute('aria-current');
  });

  it('renders tab links through the router when given renderLink', () => {
    const renderLink = vi.fn(({ href, ...props }: { href: string }) => <a data-router="yes" href={`/app${href}`} {...props} />);
    render(<TabBar renderLink={renderLink} items={[{ href: '/today', label: 'Today', icon: House, current: true }]} />);
    const link = screen.getByRole('link', { name: 'Today' });
    expect(link).toHaveAttribute('data-router', 'yes');
    expect(link).toHaveAttribute('href', '/app/today');
    expect(link).toHaveAttribute('aria-current', 'page');
  });

  it('person tabs are tabs that show the name, never colour alone', () => {
    render(
      <div role="tablist" aria-label="Family">
        <PersonTab name="Ada" color="sky" points={12} selected />
        <PersonTab name="Ezra" color="sage" />
      </div>,
    );
    expect(screen.getByRole('tab', { name: /Ada/ })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: /Ezra/ })).toHaveAttribute('aria-selected', 'false');
  });
});

describe('actions and forms', () => {
  it('buttons never submit by accident and default to secondary', () => {
    render(<Button>Save for later</Button>);
    const button = screen.getByRole('button', { name: 'Save for later' });
    expect(button).toHaveAttribute('type', 'button');
    expect(button).toHaveClass('oc-button--secondary');
  });

  it('a link button is a link', () => {
    render(
      <ButtonLink variant="primary" href="/login">
        Sign in
      </ButtonLink>,
    );
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveClass('oc-button', 'oc-button--primary');
  });

  it('inputs are named by their label and described by hint, then error', () => {
    render(<Input label="Points" hint="Whole points." error="Enter a number." />);
    const input = screen.getByLabelText('Points');
    expect(input).toHaveAccessibleDescription('Whole points. Enter a number.');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a number.');
  });

  it('a textarea is wired the same way', () => {
    render(<Textarea label="Description" hint="Shown under the name." />);
    const textarea = screen.getByRole('textbox', { name: 'Description' });
    expect(textarea.tagName).toBe('TEXTAREA');
    expect(textarea).toHaveAccessibleDescription('Shown under the name.');
    expect(textarea).not.toHaveAttribute('aria-invalid');
  });

  it('a field links its visible label to the native control', () => {
    render(
      <Field label="Cost in points" htmlFor="cost">
        <input id="cost" />
      </Field>,
    );
    expect(screen.getByLabelText('Cost in points')).toHaveAttribute('id', 'cost');
  });

  it('a segmented control is a labelled group of pressed buttons', async () => {
    const onChange = vi.fn();
    render(
      <SegmentedControl
        label="Which rewards"
        value="active"
        onChange={onChange}
        options={[
          { value: 'active', label: 'Available' },
          { value: 'archived', label: 'Archived' },
        ]}
      />,
    );
    const group = screen.getByRole('group', { name: 'Which rewards' });
    expect(within(group).getByRole('button', { name: 'Available' })).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(within(group).getByRole('button', { name: 'Archived' }));
    expect(onChange).toHaveBeenCalledWith('archived');
  });
});

describe('structure', () => {
  it('a page header has exactly one h1', () => {
    render(<PageHeader title="Rewards" description="What points can buy." actions={<Button>Add reward</Button>} />);
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 1, name: 'Rewards' })).toBeInTheDocument();
  });

  it('a section is labelled by its heading, with the count read after a pause', () => {
    render(
      <Section title="Waiting for you" count={2}>
        <p>rows</p>
      </Section>,
    );
    expect(screen.getByRole('region', { name: /^Waiting for you, ?2$/ })).toBeInTheDocument();
  });

  it('list rows are list items with a heading', () => {
    render(
      <List>
        <ListRow title="Tablet time" meta="20 points" />
      </List>,
    );
    expect(screen.getByRole('listitem')).toContainElement(screen.getByRole('heading', { level: 3, name: 'Tablet time' }));
  });
});

describe('feedback', () => {
  it('errors are alerts, other notices are polite', () => {
    render(
      <>
        <Notice tone="error">Could not save.</Notice>
        <Notice tone="attention" title="Was this change saved?">
          Check first.
        </Notice>
      </>,
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Could not save.');
    expect(screen.getByRole('status')).toHaveTextContent('Was this change saved?');
  });

  it('a healthy connection shows nothing; a degraded one says so', () => {
    const { container } = render(<StatusPill status="live" />);
    expect(container).toBeEmptyDOMElement();
    render(<StatusPill status="offline" />);
    expect(screen.getByRole('status')).toHaveTextContent('Offline — changes paused');
  });

  it('toast and empty state carry their text', () => {
    render(
      <>
        <Toast>Nice, Ada! +5 points</Toast>
        <EmptyState icon={Gift} title="No rewards yet" action={<Button>Add reward</Button>} />
        <ComingLaterTile icon={CalendarDays} title="Calendar" />
      </>,
    );
    expect(screen.getByRole('status')).toHaveTextContent('Nice, Ada! +5 points');
    expect(screen.getByText('No rewards yet')).toBeInTheDocument();
    expect(screen.getByText('Coming later')).toBeInTheDocument();
  });
});

describe('today', () => {
  it('a person card pairs the name with points and progress', () => {
    render(
      <PersonCard name="Ada" color="sky" points={12} completed={2} total={4}>
        Tasks
      </PersonCard>,
    );
    const card = screen.getByRole('article', { name: 'Ada’s today'.replace('’', "'") });
    expect(card).toHaveTextContent('2 of 4 done');
    const bar = within(card).getByRole('progressbar', { name: 'Ada: 2 of 4 done' });
    expect(bar).toHaveAttribute('value', '2');
    expect(bar).toHaveAttribute('max', '4');
  });

  it('check circles have checkbox, busy and retry semantics', () => {
    render(
      <>
        <CheckCircle label="Brush teeth" checked={false} />
        <CheckCircle label="Saving" checked={false} status="sending" />
        <CheckCircle label="Done" checked status="complete" onClick={() => undefined} />
        <CheckCircle label="Try again" status="failed" />
      </>,
    );
    expect(screen.getByRole('checkbox', { name: 'Brush teeth' })).not.toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Saving' })).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByRole('checkbox', { name: 'Done' })).toBeChecked();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
  });

  it('task rows keep pending distinct from confirmed, and celebrate only when asked', () => {
    const { rerender } = render(<TaskRow title="Feed the cat" status="pending" onAction={() => undefined} />);
    expect(screen.getByText('Waiting for approval')).toBeInTheDocument();
    rerender(<TaskRow title="Feed the cat" status="complete" />);
    expect(screen.getByText('Completed · confirmed')).toBeInTheDocument();
    rerender(<TaskRow title="Make the bed" check={{ status: 'complete', checked: true, label: 'Done' }} />);
    expect(screen.getByRole('article')).not.toHaveClass('oc-task-row--celebrate');
    rerender(<TaskRow title="Make the bed" celebrate check={{ status: 'complete', checked: true, label: 'Done' }} />);
    expect(screen.getByRole('article')).toHaveClass('oc-task-row--celebrate');
  });
});

describe('rewards', () => {
  it('a reward says how far there is to go in text', () => {
    render(<RewardTile name="Movie night" cost={50} balance={12} />);
    const tile = screen.getByRole('button', { name: /Movie night/ });
    expect(tile).toHaveAccessibleName('Movie night 50 points 38 more points');
    expect(tile).not.toHaveClass('oc-reward-tile--ready');
  });

  it('a reward within reach says Ready', () => {
    render(<RewardTile name="Choose a game" cost={4} balance={5} />);
    const tile = screen.getByRole('button', { name: /Choose a game/ });
    expect(tile).toHaveClass('oc-reward-tile--ready');
    expect(tile).toHaveTextContent('Ready');
  });
});

describe('screen time', () => {
  it('formats countdowns for the eye and the ear', () => {
    expect(countdownLabel(1499)).toBe('24:59');
    expect(countdownLabel(3723)).toBe('1:02:03');
    expect(countdownLabel(-5)).toBe('0:00');
    expect(countdownSpoken(1499)).toBe('25 minutes left');
    expect(countdownSpoken(3660)).toBe('1 hour 1 minute left');
    expect(countdownSpoken(0)).toBe('Time’s up');
  });

  it('useSurface marks the document while mounted and clears it after', () => {
    function Kiosk() {
      useSurface('kiosk');
      return null;
    }
    const { unmount } = render(<Kiosk />);
    expect(document.documentElement.dataset.surface).toBe('kiosk');
    unmount();
    expect(document.documentElement.dataset.surface).toBeUndefined();
  });

  it('says point for one and points otherwise, everywhere', () => {
    expect([0, 1, -1, 2, 12].map((n) => pointsLabel(n))).toEqual(['0 points', '1 point', '-1 point', '2 points', '12 points']);
    expect(pointsLabel(-3, '−3')).toBe('−3 points');
    const { rerender } = render(<PointsBadge points={1} />);
    expect(document.querySelector('.oc-points-badge')).toHaveTextContent('1 point');
    rerender(<PointsBadge points={4} />);
    expect(document.querySelector('.oc-points-badge')).toHaveTextContent('4 points');
  });

  it('a countdown is a silent timer named in whole minutes', () => {
    const { rerender } = render(<Countdown seconds={61} />);
    const timer = screen.getByRole('timer', { name: '2 minutes left' });
    expect(timer).toHaveTextContent('1:01 left');
    expect(timer).toHaveAttribute('aria-live', 'off');
    rerender(<Countdown seconds={0} />);
    expect(screen.getByRole('timer')).toHaveTextContent('Time’s up');
  });

  it('a countdown is marked low from five minutes, or from lowAt', () => {
    const { rerender } = render(<Countdown seconds={301} />);
    expect(screen.getByRole('timer')).not.toHaveClass('oc-countdown--low');
    rerender(<Countdown seconds={300} />);
    expect(screen.getByRole('timer')).toHaveClass('oc-countdown--low');
    rerender(<Countdown seconds={500} lowAt={600} size="hero" />);
    expect(screen.getByRole('timer')).toHaveClass('oc-countdown--low', 'oc-countdown--hero');
    rerender(<Countdown seconds={0} />);
    expect(screen.getByRole('timer')).toHaveClass('oc-countdown--ended');
    expect(screen.getByRole('timer')).not.toHaveClass('oc-countdown--low');
  });

  it('draws a QR code from its module matrix, with a quiet zone and a name', () => {
    render(
      <QrCode
        label="Pairing code"
        modules={[
          [true, false],
          [false, true],
        ]}
      />,
    );
    const svg = screen.getByRole('img', { name: 'Pairing code' });
    expect(svg).toHaveAttribute('viewBox', '0 0 10 10');
    expect(svg.querySelector('path')).toHaveAttribute('d', 'M4 4h1v1h-1zM5 5h1v1h-1z');
  });
});

describe('overlays', () => {
  it('a sheet is a labelled dialog that closes from its button', async () => {
    const onClose = vi.fn();
    render(
      <Sheet open title="Family settings" description="For adults." onClose={onClose}>
        Settings
      </Sheet>,
    );
    const dialog = screen.getByRole('dialog', { name: 'Family settings' });
    expect(dialog).toHaveAccessibleDescription('For adults.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('a confirm dialog acts only on its confirm button', async () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(
      <ConfirmDialog open tone="danger" title="Archive Tablet time?" confirmLabel="Archive" onConfirm={onConfirm} onCancel={onCancel} />,
    );
    const dialog = screen.getByRole('alertdialog', { name: 'Archive Tablet time?' });
    await userEvent.click(within(dialog).getByRole('button', { name: 'Keep it' }));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(onConfirm).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Archive' }));
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(within(dialog).getByRole('button', { name: 'Archive' })).toHaveClass('oc-button--danger');
  });
});

describe('slice one: Today and pairing', () => {
  it('a task group is a titled section that collapses to Done', () => {
    const { rerender } = render(
      <TaskGroup title="Morning" icon={Sunrise} status="3 points">
        <TaskRow title="Make the bed" check={{ status: 'open', checked: false, label: 'Make the bed' }} />
      </TaskGroup>,
    );
    expect(screen.getByRole('heading', { level: 3, name: 'Morning' })).toBeInTheDocument();
    expect(screen.getByText('3 points')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Make the bed' })).toBeInTheDocument();
    rerender(
      <TaskGroup title="Morning" done message="Nice work.">
        <TaskRow title="Make the bed" check={{ status: 'open', checked: false, label: 'Make the bed' }} />
      </TaskGroup>,
    );
    expect(screen.getByText('Done')).toBeInTheDocument();
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent('Nice work.');
  });

  it('a connection banner says when data was last updated, and nothing when live', () => {
    const { rerender, container } = render(<ConnectionBanner status="stale" lastUpdated="4:12 pm" />);
    expect(screen.getByRole('status')).toHaveTextContent('This may be out of date');
    expect(screen.getByRole('status')).toHaveTextContent('Last updated 4:12 pm.');
    rerender(<ConnectionBanner status="live" lastUpdated="4:12 pm" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('a person with nothing on the list shows no progress', () => {
    render(<PersonCard name="Noor" color="lilac" points={6} completed={0} total={0} />);
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.queryByText(/0 of 0/)).not.toBeInTheDocument();
  });

  it('pending waits for an adult and is not busy; sending is busy', () => {
    const { rerender } = render(<TaskRow title="Tidy the hallway" status="pending" />);
    expect(screen.getByRole('article')).not.toHaveAttribute('aria-busy');
    expect(screen.getByText('Waiting for approval')).toBeInTheDocument();
    rerender(<TaskRow title="Tidy the hallway" status="sending" />);
    expect(screen.getByRole('article')).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByText('Saving…')).toBeInTheDocument();
  });

  it("a row shows a parent's reason in full, and a group can wait for approval", () => {
    const reason = `Not approved yet: ${'a'.repeat(200)}`;
    render(
      <TaskGroup title="Morning" pending>
        <TaskRow title="Make the bed" message={reason} />
      </TaskGroup>,
    );
    expect(screen.getByText(reason)).toBeInTheDocument();
    expect(screen.getByText('Waiting for approval')).toBeInTheDocument();
  });

  it('the people board is a named region', () => {
    render(<PeopleBoard>columns</PeopleBoard>);
    expect(screen.getByRole('region', { name: 'Today by person' })).toBeInTheDocument();
  });

  it('a code input takes the code as the server reads it, with no autocomplete', () => {
    render(<CodeInput label="Family code" />);
    const input = screen.getByRole('textbox', { name: 'Family code' });
    expect(input).toHaveAttribute('minlength', '6');
    // Browsers compile `pattern` anchored, with the v flag.
    const pattern = new RegExp(`^(?:${input.getAttribute('pattern') ?? ''})$`, 'v');
    for (const ok of ['K7Q2XM', 'k7q2xm', 'K7Q-2XM', 'K7Q 2XM']) expect(pattern.test(ok)).toBe(true);
    for (const bad of ['K7Q2X', 'K7Q2XMM', 'K0Q2XM', 'KOQ2XM', 'K1Q2XM', 'KIQ2XM', 'KLQ2XM']) expect(pattern.test(bad)).toBe(false);
    expect(input).toHaveAttribute('autocomplete', 'off');
    expect(input).toHaveAttribute('spellcheck', 'false');
  });
});

describe('manage navigation', () => {
  const groups = [
    {
      label: 'Family',
      items: [
        { href: '/manage/inbox', label: 'Inbox', icon: Inbox, badge: 3 },
        { href: '/manage/children', label: 'Children', icon: Users, current: true },
      ],
    },
  ];

  it('marks the current page, speaks the badge, and keeps the shortcut on the phone bar', () => {
    render(<ManageNav groups={groups} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />);
    const nav = screen.getByRole('navigation', { name: 'Adult management' });
    expect(within(nav).getAllByRole('link', { name: 'Children' })[0]).toHaveAttribute('aria-current', 'page');
    expect(within(nav).getAllByLabelText('3 waiting').length).toBeGreaterThan(0);
    expect(within(nav).getAllByRole('link', { name: /Inbox/ })).toHaveLength(2);
    expect(within(nav).getByRole('button', { name: 'Menu' })).toBeInTheDocument();
  });
});

describe('slice two: inbox', () => {
  it('a choice group answers with one tap, speaks full names, and locks while sending', async () => {
    const chosen: string[] = [];
    const choices = [
      { value: 'parents', label: 'Parents', variant: 'primary' as const },
      { value: 'ezra', label: 'Ezra', detail: 'no time', disabled: true },
      { value: 'ada', label: 'Ada', detail: '25 min' },
    ];
    const { rerender } = render(<ChoiceGroup label="Who’s watching?" choices={choices} onChoose={(v) => chosen.push(v)} />);
    expect(screen.getByRole('group', { name: 'Who’s watching?' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Ezra no time' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Ada 25 min' }));
    expect(chosen).toEqual(['ada']);
    rerender(<ChoiceGroup label="Who’s watching?" choices={choices} onChoose={(v) => chosen.push(v)} busy />);
    for (const button of screen.getAllByRole('button')) expect(button).toBeDisabled();
  });
});

describe('qrModulesFromRows', () => {
  it('reads the server’s rows: only "1" is dark', () => {
    expect(qrModulesFromRows(['101', '0x1'])).toEqual([
      [true, false, true],
      [false, false, true],
    ]);
  });
});
