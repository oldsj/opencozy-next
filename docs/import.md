# Importing the old app

`app import` copies a household from the old OpenCozy app into a new, empty OpenCozy
installation, in one transaction. It supports exactly one old version: commit `d7fead8`,
whose last migration is `0045_parent_push_mute`. A source at any other version is refused.

This page lists what is carried, what is deliberately left behind and why, and how to read
the reconciliation report. James signs off on the "Not carried" and "Changed on purpose"
lists before cutover.

## How it works

- **The source is read-only.** It is a restored backup, never the live database. The
  import opens it with `default_transaction_read_only=on`, in one `REPEATABLE READ READ ONLY`
  snapshot, and only ever selects from it.
- **The target must be migrated and empty.** The import runs as the schema owner
  (`MIGRATION_DATABASE_URL`) with every trigger and constraint on, so the new schema checks
  every row. For example, the points ledger stays append-only and never goes below zero.
- **The whole import is one transaction.** If any line or check fails, or anything is still
  pending, nothing is written.
- **IDs are kept** wherever a row is carried one to one, so links between rows survive and
  devices that remember an ID keep working.
- **The output is counts only.** Errors name the table, the constraint and at most a row's
  UUID, never names or other contents.

## Commands

Rehearse first, with fictional data: `make import-rehearsal` runs two dry runs, a real import
and a second import against a throwaway Postgres, and checks each child's points and banked time
independently. It must pass with nothing pending.

The new installation's environment is the same as for `app serve` (every setting is in
[deploy.md](deploy.md)):

| Variable | What it is |
| --- | --- |
| `DATABASE_URL` | The new database, as the app's runtime role |
| `MIGRATION_DATABASE_URL` and `DATABASE_RUNTIME_ROLE` | The schema owner, which migrates and imports, and the runtime role it grants to |
| `OPENCOZY_SECRET_KEYS` | Seals imported calendar links and integration secrets |
| `WEBAUTHN_RP_ID` and `WEBAUTHN_ORIGINS` | Must be the old app's hostname and origin, or imported passkeys stop working (see Passkeys) |

Put the source password in `PGPASSWORD` or a passfile, not in the URL.

```bash
# 1. Restore the old app's backup into a scratch database (never the live one).

# 2. Dry run: imports everything, prints the report, rolls back. Run it as often as you like.
PGPASSWORD=… app import --dry-run \
  --source-url 'postgres://opencozy@old-restore.example.test:5432/opencozy?sslmode=require' \
  --old-auth-secret-file ./old-auth-secret      # optional, see below

# 3. Real import into the fresh installation (stop the old app first; same flags, no --dry-run).
PGPASSWORD=… app import \
  --source-url 'postgres://opencozy@old-restore.example.test:5432/opencozy?sslmode=require' \
  --old-auth-secret-file ./old-auth-secret

# 4. Start the app, then run the read-only smoke test against it.
scripts/smoke.sh https://cozy.example.test
```

- **Google calendars** keep working only with the old app's OAuth client: reuse its client ID
  and secret (deploy.md, Google calendars). With a new client every imported account shows "Sign
  in again".
- **`--old-auth-secret-file`** is a file (`chmod 600`) holding the old app's
  `BETTER_AUTH_SECRET`. The import reads it once, uses it to open the old Google refresh tokens, and
  seals them under `OPENCOZY_SECRET_KEYS`; it is never stored. Without it, Google accounts arrive
  as "connect again" and a Remark at the end of the report says so. (iCloud and Home Assistant
  are not carried: see Not carried.)
- **Exit status.** A run fails, and writes nothing, in any of these cases:
  - the reconciliation has a mismatch ("found mismatches; nothing was written"), in a dry
    run too;
  - something is still pending ("some old data has no mapping yet"), for a real import only.
    A dry run with pending lines ends with "Dry run: … rolled back." and exit status 0, so
    check its STATUS column;
  - the target already has rows ("the target is not empty");
  - the source is at another version.

  A real import that succeeds ends with "Imported and committed."
- **A second real import into the same installation is refused.** To start over, use a fresh
  database.

## Reading the report

The report has a table of old tables, then a table of checks, then Remarks.

```text
OLD TABLE        ROWS  NEW TABLE     ROWS  STATUS       NOTES
reward           6     reward        6     ok
time_request     2                         not carried  1 answered (a yes is in the balance), 1 still waiting …

CHECK                                   WANT    GOT   STATUS  NOTES
banked iPad seconds, all children       900     900   ok
```

The old tables table has these columns:

- **ROWS** (left): the rows in the old table.
- **NEW TABLE** and **ROWS** (right): where they went and how many rows that table now has.
  The two counts differ when the notes say so, for example when rows were dropped on purpose,
  merged into one balance, or split into two tables.
- **STATUS** is one of:
  - `ok`: carried, and the number of rows written is the number expected.
  - `not carried`: deliberately left behind; the note says why.
  - `pending`: something in this table has no mapping yet. Either it has rows nobody maps,
    or it has a column no mapping reads, which the note names. A new old column is never
    lost silently. A real import refuses while anything is pending.
  - `MISMATCH`: the rows written aren't the rows expected. The run fails.
- **NOTES**: counts of what was dropped, merged or changed, and why.

Read the STATUS column, not the notes, when parsing: a note can contain words like
"pending" (for example "pending TV prompts").

Each check compares a WANT computed from the old database with a GOT computed from the new
one. Anything but `ok` fails the run. A check can also be `pending` while the import of what
it checks hasn't run yet.

## What is carried

| Old | New | How |
| --- | --- | --- |
| `household` | `household` | ID, name, time zone, times, blocked websites (games and game portals), and the locked-iPad message |
| `child` | `child` | ID, name, colour, times and allowed hours (minutes after local midnight). Allowed hours the old app had only partly set (which both apps read as none) arrive as none, counted. |
| `auth_user`, `household_owner`, `auth_account` (password) | `adult` | Adults with a household, with email, name, password and phone push mute. Old scrypt hashes are rehashed to argon2id on the first sign-in. The old user ID becomes the passkey user handle. |
| `parent_passkey` | `passkey` | ID, name, credential ID, public key, sign counter, transports and backed-up flag. "Backup eligible" is unknown until the first sign-in sets it. |
| `device` | `device` | Child devices: ID, child, name, platform, key, times, and revoked ones as revoked. A paired iPad keeps working without pairing again. |
| `service_credential`, `…_child`, `…_award_rule` | `credential`, `credential_child`, `credential_award_rule` | Service credentials (learning games) with their children and rules |
| `assistant_credential` | `credential` (assistant) | Assistant credentials, each old scope under its new name, plus the read scopes the old app always gave an assistant |
| `award_rule`, `external_identity_link`, `external_award` | `award_rule`, `learning_link`, `learning_award` | Learning rules, each child's game link, and every award with its source event ID, so a retried event is never paid twice |
| `point_ledger` | `point_ledger` | Every row, with its ID, child, points and time, written oldest first under the new rules |
| `chore_award_reversal`, `chore_active_award` | `point_ledger.reverses_id`, `active_award` | An undo points at what it undoes; the awards in effect are rebuilt and compared with the old list |
| `chore_definition`, `routine_definition` | `chore`, `routine` + `routine_step` | ID, child, title, points, version and times. Old weekdays become the new weekday mask; routine steps keep their IDs, in order. |
| `chore_occurrence`, `routine_occurrence` | `occurrence` | Each occurrence is keyed by its chore or routine and its household day, the same key the server uses. Its state follows its history: complete while paid, otherwise open, with the reason if an adult sent it back. Ticked routine steps are kept. |
| `completion_approval` (decided) | `approval` | ID, kind, state, reason, times and who decided |
| `reward` | `reward` | ID, cost, version, times, screen-time minutes and target (the old "personal" target is the iPad) |
| `reward_redemption` (of a reward, decided) | `redemption` | ID, child, reward, title, cost, minutes, version and times. Fulfilled stays fulfilled and cancelled stays cancelled; each names its own spend in the ledger. |
| `personal_credit` | `screen_time_credit` | **Banked iPad time, one balance per child**: what the old credits still held, in credits of at most a day |
| `tv_credit` | `tv_credit` | **Banked TV time, one balance per child**, the same way |
| `gated_tv`, `gated_tv_mac` | `tv`, `tv_mac` | TVs with their IDs, room, open hours, Home Assistant entity, and MAC addresses |
| `tv_session` (ended) | `tv_session` | Finished sessions, with who started and ended them |
| `tv_open_event` | `tv_open_event` | The history of a parent opening or closing a TV |
| `calendar_google_connection` | `calendar_google_connection` | Google accounts, with their refresh token sealed (or "connect again" without `--old-auth-secret-file`, or if the old app already needed it) |
| `calendar_source`, `calendar_series`, `calendar_assignment`, `calendar_assignment_person` | `calendar_source`, `calendar_series`, `calendar_assignment`, `calendar_assignment_child` | Calendar feeds (links sealed), their events' series, and who each event is for |
| `recipe`, `meal_plan_entry` | `recipe`, `meal` | Recipes and the meal plan |

## Not carried

Each of these is counted in the report.

| Old | Why |
| --- | --- |
| `auth_session`, `display_session`, `parent_session` (with phone push tokens), `device_web_session` | Sessions: adults sign in again, wall displays pair again, and phones get a new build and sign in |
| `auth_verification`, `webauthn_challenge`, `device_nonce`, `display_pairing`, `device_pairing`, `owner_bootstrap` | Short-lived security state |
| `command` | The old command log; the new app keeps its own |
| `auth_account` (other than password) | OIDC links: none exist in production |
| `auth_user` without a household | Not an adult of any household |
| `device` push registrations | The new kids' app registers again |
| `completion_approval` still waiting (James) | The chore or routine is open again; the child marks it done again |
| `completion_approval` withdrawn | The child took it back before anyone decided |
| `reward_redemption` still waiting (James) | Its spend isn't carried either, so **the points go back to the child**, who can ask again |
| `reward_redemption` grants and "yes" answers | They had no reward and no points; their time is in the banked balance |
| `screen_time_grant` and `screen_time_grant_cut` (unlock windows) | The history of time already spent; a cut's rest is already back in the balance. A window still open at import ends, and its unused time is counted, not given back (James). |
| `time_request` | A waiting request is dropped and the child asks again (James); a "yes" is already in the balance |
| `screen_time_event` | The iPad shield event log |
| `tv_session` still open (James) | The TV goes offline at cutover and a new session starts; the time was charged as it ran, so the balance is right |
| `tv_prompt` | Pending prompts to start a TV |
| `calendar_occurrence` | Each feed's first refresh rebuilds the events |
| `calendar_assignment` cleared | The old app ignored them |
| Google calendars whose account is gone, with their series and assignments | Nothing can refresh them; connect the account again (sign-off: who those events were for) |
| Duplicate calendar links | One of each is carried |
| `home_assistant_connection`, `home_assistant_entity` | Home Assistant is a later slice (15): connect it again after cutover |
| `icloud_connection`, `icloud_calendar`, `icloud_resource`, `icloud_series`, `icloud_occurrence` | iCloud calendars are a later slice (13b): connect them again after cutover; the first refresh rebuilds the events |
| `dashboard_view` | Dashboard views are a later slice (16): set them up again after cutover |

Columns dropped on purpose are listed in the mapping code next to `columnsRead`. Any other
old column makes its table `pending`.

## Changed on purpose

- **One-off chores arrive archived.** The new app has only repeating chores; the report counts
  those never done, which you add again.
- **Banked iPad and TV time is one balance per child** (James), shown as a grant from no one in
  particular. A parent can still take back what's left.
- **An imported screen-time or TV redemption can't be cancelled for a partial refund.** It has
  no credit of its own, because its time is in the balance. The screens don't offer cancel on it
  (each redemption says whether it can be cancelled).
- **History shows a chore's current title**, not the title it had then.
- **Calendar feeds get colours in turn.** The old app had none.
- **TVs' power and router attempts start as unknown** until Home Assistant reports.

## Checks

| Check | What it compares |
| --- | --- |
| `<table> rows read` | Old rows against rows carried plus rows dropped on purpose, for tables where some rows are dropped |
| `point_ledger rows read = carried + spends on waiting rewards` | Every old ledger row is accounted for |
| `children whose balance matches (+ waiting rewards' points)` | Each child's points: old balance, plus spends on rewards still waiting, equals the new balance |
| `points held, all children (+ waiting rewards' points)` | The same, in total |
| `active chore awards rebuilt = old` | The chore awards in effect, rebuilt from the ledger, equal the old list |
| `active awards (awards not reversed)` | Every award not undone is in effect, once |
| `ledger rows whose occurrence exists`, `… redemption exists`, `… learning_award exists` | Every ledger row's source was carried, for the same child |
| `ledger rows whose occurrence was carried` | Chore and routine ledger rows found their occurrence |
| `complete occurrences = active awards` | An occurrence is complete exactly when it is paid |
| `routine steps`, `routine steps written` | Every old step became a step |
| `completion_approval rows read = carried + waiting + withdrawn` | Every approval is accounted for |
| `reward_redemption rows read = carried + waiting + grants` | Every redemption is accounted for |
| `redemptions whose ledger_id is their spend` | Each redemption names its own spend, for its child |
| `parent_passkey rows read = carried + of adults not carried` | Every passkey is accounted for |
| `children whose banked iPad seconds match`, `banked iPad seconds, all children` | Banked iPad time per child and in total, before and after |
| `children whose banked TV seconds match`, `banked TV seconds, all children` | The same for TV time |
| `children with allowed hours` | Every child with allowed hours has them |
| `blocked websites` | Every blocked website was carried |

## Passkeys and devices after cutover

- **Passkeys** work only if the new installation keeps the old hostname: `WEBAUTHN_RP_ID`
  must be the old hostname (or a parent domain of it), and `WEBAUTHN_ORIGINS` must include the
  old origin. The smoke test checks that the passkey domain fits the site. Otherwise adults sign
  in with their password and add a passkey again.
- **An imported iPad** stays paired if the new kids' build keeps the same bundle ID, keychain
  item and host. If someone pairs it again anyway, revoke the old entry in Manage → Devices.

## Limits

The new app accepts less for new input than the old app did. The database allows the old
limits (migrations 00023 and 00024), so old rows are carried as they are, without shortening:

| | Old app | New input |
| --- | --- | --- |
| Chore, routine, step, approval and reward titles | 160 characters | 80 |
| Points per chore or routine | 1,000,000 | 1,000 |
| Steps per routine | 32 | 20 |
| Reward cost | 1,000,000 | 100,000 |

An adult who edits such a chore, routine or reward brings it within the new limits then.

## Still pending

Nothing today: every old table is carried or deliberately not carried. An old table, or column,
that no mapping reads is `pending`, and a real import refuses while it has rows, so nothing new in
the old app can be lost silently.
