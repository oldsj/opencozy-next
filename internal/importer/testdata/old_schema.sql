-- The old app's schema at commit e717170 (migration 0044_screen_time_grant_cut_guards),
-- for the importer's fixture tests. Made with pg_dump --schema-only --no-owner --no-privileges
-- --no-comments -n opencozy after applying the old migrations 0000-0044, plus the
-- schema_migrations rows the old migrator writes (version and SHA-256 of each file). No rows of
-- any other table: old_seed.sql adds fictional ones.
SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;
CREATE SCHEMA opencozy;
CREATE FUNCTION opencozy.delete_calendar_series_assignments() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  DELETE FROM calendar_assignment WHERE household_id = OLD.household_id AND series_id = OLD.id;
  RETURN OLD;
END $$;
CREATE FUNCTION opencozy.guard_screen_time_grant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Screen-time grants cannot be removed'; END IF;
  IF TG_OP = 'UPDATE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Screen-time grants cannot change'; END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('screen_time_grant/' || NEW.household_id || '/' || NEW.child_id || '/' || NEW.target, 0));
  IF NEW.ends_at > NEW.starts_at AND EXISTS (
    SELECT 1 FROM screen_time_grant g
    LEFT JOIN screen_time_grant_cut c ON c.household_id = g.household_id AND c.grant_id = g.id
    CROSS JOIN LATERAL (SELECT COALESCE(LEAST(c.cut_at, g.ends_at), g.ends_at) AS ends_at) e
    WHERE g.household_id = NEW.household_id AND g.child_id = NEW.child_id AND g.target = NEW.target
      AND g.id <> NEW.id AND e.ends_at > g.starts_at
      AND g.starts_at < NEW.ends_at AND NEW.starts_at < e.ends_at) THEN
    RAISE EXCEPTION USING ERRCODE = '23P01', MESSAGE = 'Screen-time grants for a child and target cannot overlap';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION opencozy.guard_screen_time_grant_cut() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE g screen_time_grant%ROWTYPE;
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Screen-time cuts cannot be removed'; END IF;
  IF TG_OP = 'UPDATE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Screen-time cuts cannot change'; END IF;
  SELECT * INTO g FROM screen_time_grant WHERE household_id = NEW.household_id AND id = NEW.grant_id;
  PERFORM pg_advisory_xact_lock(hashtextextended('screen_time_grant/' || g.household_id || '/' || g.child_id || '/' || g.target, 0));
  IF g.child_id IS DISTINCT FROM NEW.child_id THEN
    RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'A screen-time cut belongs to its window''s child';
  END IF;
  IF NEW.cut_at < g.starts_at OR NEW.cut_at >= g.ends_at THEN
    RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'A screen-time cut must fall inside its window';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION opencozy.protect_completion_approval() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Approval history cannot be removed'; END IF;
  IF (to_jsonb(NEW) - 'state' - 'version' - 'decided_at' - 'decided_by' - 'reason' - 'ledger_id')
      IS DISTINCT FROM (to_jsonb(OLD) - 'state' - 'version' - 'decided_at' - 'decided_by' - 'reason' - 'ledger_id')
    OR OLD.state <> 'pending' OR NEW.state = 'pending' OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Invalid approval transition';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION opencozy.protect_external_identity_link() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'External identity history cannot be removed'; END IF;
  IF (to_jsonb(NEW) - 'unlinked_at') IS DISTINCT FROM (to_jsonb(OLD) - 'unlinked_at')
    OR OLD.unlinked_at IS NOT NULL OR NEW.unlinked_at IS NULL THEN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Invalid external identity transition';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION opencozy.protect_reward_redemption() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP <> 'UPDATE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Redemption history cannot be removed'; END IF;
  IF (to_jsonb(NEW) - 'state' - 'version' - 'settled_at' - 'fulfilled_at' - 'refunded_points')
      IS DISTINCT FROM (to_jsonb(OLD) - 'state' - 'version' - 'settled_at' - 'fulfilled_at' - 'refunded_points')
    OR NEW.version <> OLD.version + 1
    OR NOT ((OLD.state = 'pending' AND NEW.state IN ('fulfilled','canceled'))
      OR (OLD.state = 'fulfilled' AND NEW.state = 'canceled' AND OLD.screen_time_minutes_snapshot IS NOT NULL
        AND NEW.fulfilled_at IS NOT DISTINCT FROM OLD.fulfilled_at)) THEN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Invalid redemption transition';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION opencozy.protect_routine_occurrence() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Routine history cannot be removed'; END IF;
  IF TG_OP = 'UPDATE' AND ((to_jsonb(NEW) - 'checked_step_ids' - 'version') IS DISTINCT FROM (to_jsonb(OLD) - 'checked_step_ids' - 'version')
    OR NEW.version <> OLD.version + 1) THEN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'Invalid routine transition';
  END IF;
  IF EXISTS (SELECT 1 FROM unnest(NEW.checked_step_ids) id WHERE id IS NULL
      OR NOT EXISTS (SELECT 1 FROM jsonb_array_elements(NEW.steps_snapshot) s WHERE (s->>'id')::uuid = id))
    OR cardinality(NEW.checked_step_ids) <> (SELECT count(DISTINCT id) FROM unnest(NEW.checked_step_ids) id) THEN
    RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'Invalid checked steps';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION opencozy.reject_history_mutation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = format('%s is append-only', TG_TABLE_NAME);
END $$;
CREATE FUNCTION opencozy.revoke_parent_sessions_for_web_session() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  UPDATE parent_session SET revoked_at = COALESCE(revoked_at, now()), push_token = NULL, push_environment = NULL, push_registered_at = NULL
  WHERE web_session_id = OLD.id AND revoked_at IS NULL;
  RETURN OLD;
END $$;
CREATE FUNCTION opencozy.routine_steps_valid(steps jsonb) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE
    AS $_$
DECLARE step jsonb; ids text[] := ARRAY[]::text[];
BEGIN
  IF jsonb_typeof(steps) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
  IF jsonb_array_length(steps) NOT BETWEEN 1 AND 32 THEN RETURN false; END IF;
  FOR step IN SELECT value FROM jsonb_array_elements(steps) LOOP
    IF jsonb_typeof(step) IS DISTINCT FROM 'object' OR NOT (step ?& ARRAY['id','title'])
      OR (step - 'id' - 'title') <> '{}'::jsonb
      OR jsonb_typeof(step->'id') IS DISTINCT FROM 'string'
      OR (step->>'id') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
      OR jsonb_typeof(step->'title') IS DISTINCT FROM 'string'
      OR length(btrim(step->>'title')) NOT BETWEEN 1 AND 160
      OR (step->>'id') = ANY(ids) THEN RETURN false; END IF;
    ids := array_append(ids, step->>'id');
  END LOOP;
  RETURN true;
END $_$;
CREATE FUNCTION opencozy.text_array_valid(items text[], max_items integer, max_len integer) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE
    AS $$
DECLARE item text;
BEGIN
  IF items IS NULL OR array_ndims(items) <> 1 THEN RETURN false; END IF;
  IF cardinality(items) NOT BETWEEN 1 AND max_items THEN RETURN false; END IF;
  FOREACH item IN ARRAY items LOOP
    IF item IS NULL OR length(btrim(item)) NOT BETWEEN 1 AND max_len THEN RETURN false; END IF;
  END LOOP;
  RETURN true;
END $$;
SET default_tablespace = '';
SET default_table_access_method = heap;
CREATE TABLE opencozy.assistant_credential (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    user_id text NOT NULL,
    name text NOT NULL,
    credential_hash text NOT NULL,
    scopes text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    revoked_at timestamp with time zone,
    CONSTRAINT assistant_credential_hash_format CHECK ((credential_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT assistant_credential_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 160))),
    CONSTRAINT assistant_credential_scopes_valid CHECK (((array_position(scopes, NULL::text) IS NULL) AND ((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 7)) AND (scopes <@ ARRAY['approvals:read'::text, 'approvals:write'::text, 'chores:log'::text, 'screen-time:grant'::text, 'tv:read'::text, 'tv:manage'::text, 'tv:control'::text]))),
    CONSTRAINT assistant_credential_tv_control_reads CHECK (((NOT ('tv:control'::text = ANY (scopes))) OR ('tv:read'::text = ANY (scopes)))),
    CONSTRAINT assistant_credential_tv_manage_reads CHECK (((NOT ('tv:manage'::text = ANY (scopes))) OR ('tv:read'::text = ANY (scopes)))),
    CONSTRAINT assistant_credential_write_reads CHECK (((NOT ('approvals:write'::text = ANY (scopes))) OR ('approvals:read'::text = ANY (scopes))))
);
CREATE TABLE opencozy.auth_account (
    id text NOT NULL,
    user_id text NOT NULL,
    account_id text NOT NULL,
    provider_id text NOT NULL,
    access_token text,
    refresh_token text,
    id_token text,
    access_token_expires_at timestamp with time zone,
    refresh_token_expires_at timestamp with time zone,
    scope text,
    password text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE opencozy.auth_session (
    id text NOT NULL,
    user_id text NOT NULL,
    token text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    ip_address text,
    user_agent text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE opencozy.auth_user (
    id text NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    email_verified boolean DEFAULT false NOT NULL,
    image text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT auth_user_email_lowercase CHECK ((email = lower(email)))
);
CREATE TABLE opencozy.auth_verification (
    id text NOT NULL,
    identifier text NOT NULL,
    value text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE opencozy.award_rule (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    activity_key text NOT NULL,
    points integer NOT NULL,
    daily_limit integer NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT award_rule_activity_key_length CHECK (((length(btrim(activity_key)) >= 1) AND (length(btrim(activity_key)) <= 160))),
    CONSTRAINT award_rule_daily_limit_bound CHECK (((daily_limit >= 1) AND (daily_limit <= 1000000))),
    CONSTRAINT award_rule_points_bound CHECK (((points >= 1) AND (points <= 1000000))),
    CONSTRAINT award_rule_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.calendar_assignment (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    series_id uuid NOT NULL,
    occurrence_key text,
    version integer DEFAULT 1 NOT NULL,
    active boolean DEFAULT true NOT NULL,
    cleared_from_version integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT calendar_assignment_clear_revision CHECK ((active OR ((cleared_from_version IS NOT NULL) AND (cleared_from_version > 0)))),
    CONSTRAINT calendar_assignment_occurrence_key_length CHECK (((occurrence_key IS NULL) OR ((length(occurrence_key) >= 1) AND (length(occurrence_key) <= 255)))),
    CONSTRAINT calendar_assignment_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.calendar_assignment_person (
    household_id uuid NOT NULL,
    assignment_id uuid NOT NULL,
    child_id uuid NOT NULL
);
CREATE TABLE opencozy.calendar_google_connection (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    google_subject text NOT NULL,
    account_email text NOT NULL,
    status text DEFAULT 'connected'::text NOT NULL,
    scope text NOT NULL,
    refresh_token_encrypted text NOT NULL,
    access_token_encrypted text,
    access_token_expires_at timestamp with time zone,
    last_error_at timestamp with time zone,
    credential_version integer DEFAULT 1 NOT NULL,
    token_refresh_lease_id uuid,
    token_refresh_lease_until timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT calendar_google_connection_credential_version CHECK ((credential_version > 0)),
    CONSTRAINT calendar_google_connection_email_length CHECK (((length(btrim(account_email)) >= 1) AND (length(btrim(account_email)) <= 320))),
    CONSTRAINT calendar_google_connection_refresh_lease CHECK (((token_refresh_lease_id IS NULL) = (token_refresh_lease_until IS NULL))),
    CONSTRAINT calendar_google_connection_scope_length CHECK (((length(btrim(scope)) >= 1) AND (length(btrim(scope)) <= 1000))),
    CONSTRAINT calendar_google_connection_status CHECK ((status = ANY (ARRAY['connected'::text, 'needs_reauth'::text, 'revoked'::text]))),
    CONSTRAINT calendar_google_connection_subject_length CHECK (((length(btrim(google_subject)) >= 1) AND (length(btrim(google_subject)) <= 255))),
    CONSTRAINT calendar_google_connection_tokens CHECK ((((length(refresh_token_encrypted) >= 1) AND (length(refresh_token_encrypted) <= 4096)) AND ((access_token_encrypted IS NULL) OR ((length(access_token_encrypted) >= 1) AND (length(access_token_encrypted) <= 4096))) AND ((access_token_expires_at IS NULL) OR isfinite(access_token_expires_at))))
);
CREATE TABLE opencozy.calendar_occurrence (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    source_id uuid NOT NULL,
    series_id uuid NOT NULL,
    recurrence_key text NOT NULL,
    all_day boolean NOT NULL,
    starts_at timestamp with time zone,
    ends_at timestamp with time zone,
    start_date date,
    end_date date,
    time_zone text,
    summary text,
    location text,
    description text,
    status text NOT NULL,
    is_override boolean NOT NULL,
    revision integer DEFAULT 1 NOT NULL,
    content_hash text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT calendar_occurrence_content_hash CHECK ((content_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT calendar_occurrence_key_format CHECK ((recurrence_key ~ '^(single|[0-9]{8}|[0-9]{8}T[0-9]{6}Z)$'::text)),
    CONSTRAINT calendar_occurrence_revision_positive CHECK ((revision > 0)),
    CONSTRAINT calendar_occurrence_status CHECK ((status = ANY (ARRAY['confirmed'::text, 'tentative'::text, 'cancelled'::text]))),
    CONSTRAINT calendar_occurrence_text_length CHECK ((((summary IS NULL) OR ((length(btrim(summary)) >= 1) AND (length(btrim(summary)) <= 500))) AND ((location IS NULL) OR ((length(btrim(location)) >= 1) AND (length(btrim(location)) <= 500))) AND ((description IS NULL) OR ((length(btrim(description)) >= 1) AND (length(btrim(description)) <= 4000))))),
    CONSTRAINT calendar_occurrence_time_shape CHECK (((all_day AND (start_date IS NOT NULL) AND (end_date IS NOT NULL) AND isfinite(start_date) AND isfinite(end_date) AND (end_date > start_date) AND (starts_at IS NULL) AND (ends_at IS NULL)) OR ((NOT all_day) AND (starts_at IS NOT NULL) AND (ends_at IS NOT NULL) AND isfinite(starts_at) AND isfinite(ends_at) AND (ends_at >= starts_at) AND (start_date IS NULL) AND (end_date IS NULL)))),
    CONSTRAINT calendar_occurrence_time_zone CHECK (((time_zone IS NULL) OR ((length(time_zone) >= 1) AND (length(time_zone) <= 100))))
);
CREATE TABLE opencozy.calendar_series (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    source_id uuid NOT NULL,
    provider_uid text NOT NULL,
    summary text,
    all_day boolean NOT NULL,
    time_zone text,
    recurring boolean NOT NULL,
    status text NOT NULL,
    revision integer DEFAULT 1 NOT NULL,
    content_hash text NOT NULL,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    removed_at timestamp with time zone,
    CONSTRAINT calendar_series_content_hash CHECK ((content_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT calendar_series_revision_positive CHECK ((revision > 0)),
    CONSTRAINT calendar_series_status CHECK ((status = ANY (ARRAY['confirmed'::text, 'tentative'::text, 'cancelled'::text]))),
    CONSTRAINT calendar_series_summary_length CHECK (((summary IS NULL) OR ((length(btrim(summary)) >= 1) AND (length(btrim(summary)) <= 500)))),
    CONSTRAINT calendar_series_time_zone CHECK (((time_zone IS NULL) OR ((length(time_zone) >= 1) AND (length(time_zone) <= 100)))),
    CONSTRAINT calendar_series_uid_length CHECK (((length(provider_uid) >= 1) AND (length(provider_uid) <= 1024)))
);
CREATE TABLE opencozy.calendar_source (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    provider text NOT NULL,
    name text NOT NULL,
    feed_url text,
    show_on_display boolean DEFAULT true NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    sync_generation integer DEFAULT 0 NOT NULL,
    last_attempt_at timestamp with time zone,
    last_success_at timestamp with time zone,
    last_changed_at timestamp with time zone,
    last_error_at timestamp with time zone,
    last_error_code text,
    consecutive_failures integer DEFAULT 0 NOT NULL,
    skipped_components integer DEFAULT 0 NOT NULL,
    refresh_lease_id uuid,
    refresh_lease_until timestamp with time zone,
    http_etag text,
    http_last_modified text,
    feed_hash text,
    applied_window_key text,
    google_connection_id uuid,
    google_calendar_id text,
    google_sync_token text,
    google_window_start_at timestamp with time zone,
    google_window_end_at timestamp with time zone,
    CONSTRAINT calendar_source_counters CHECK (((sync_generation >= 0) AND (consecutive_failures >= 0) AND (skipped_components >= 0))),
    CONSTRAINT calendar_source_error CHECK ((((last_error_code IS NULL) = (last_error_at IS NULL)) AND ((last_error_code IS NULL) OR (last_error_code = ANY (ARRAY['blocked_address'::text, 'dns_failed'::text, 'connect_failed'::text, 'tls_failed'::text, 'timeout'::text, 'too_large'::text, 'redirect_blocked'::text, 'too_many_redirects'::text, 'access_denied'::text, 'not_found'::text, 'server_error'::text, 'http_error'::text, 'invalid_calendar'::text, 'too_many_events'::text, 'internal'::text, 'token_expired'::text, 'access_revoked'::text, 'not_connected'::text, 'rate_limited'::text]))))),
    CONSTRAINT calendar_source_feed_hash CHECK (((feed_hash IS NULL) OR (feed_hash ~ '^[a-f0-9]{64}$'::text))),
    CONSTRAINT calendar_source_feed_url CHECK ((((provider = 'ics'::text) = (feed_url IS NOT NULL)) AND ((feed_url IS NULL) OR ((length(feed_url) >= 1) AND (length(feed_url) <= 2048))))),
    CONSTRAINT calendar_source_google_fields CHECK ((((provider = 'google'::text) = ((google_connection_id IS NOT NULL) AND (google_calendar_id IS NOT NULL))) AND ((google_calendar_id IS NULL) OR ((length(google_calendar_id) >= 1) AND (length(google_calendar_id) <= 512))) AND ((google_sync_token IS NULL) OR ((length(google_sync_token) >= 1) AND (length(google_sync_token) <= 2048))) AND ((google_window_start_at IS NULL) = (google_window_end_at IS NULL)) AND ((google_window_start_at IS NULL) OR (google_window_end_at > google_window_start_at)))),
    CONSTRAINT calendar_source_http_cache CHECK ((((http_etag IS NULL) OR ((length(http_etag) >= 1) AND (length(http_etag) <= 512))) AND ((http_last_modified IS NULL) OR ((length(http_last_modified) >= 1) AND (length(http_last_modified) <= 128))))),
    CONSTRAINT calendar_source_lease CHECK (((refresh_lease_id IS NULL) = (refresh_lease_until IS NULL))),
    CONSTRAINT calendar_source_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT calendar_source_provider CHECK ((provider = ANY (ARRAY['ics'::text, 'google'::text]))),
    CONSTRAINT calendar_source_version_positive CHECK ((version > 0)),
    CONSTRAINT calendar_source_window_key CHECK (((applied_window_key IS NULL) OR ((length(applied_window_key) >= 1) AND (length(applied_window_key) <= 200))))
);
CREATE TABLE opencozy.child (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    name text NOT NULL,
    color text DEFAULT 'rose'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    school_night_start integer,
    school_night_end integer,
    weekend_start integer,
    weekend_end integer,
    CONSTRAINT child_allowed_hours CHECK ((((school_night_start IS NULL) AND (school_night_end IS NULL) AND (weekend_start IS NULL) AND (weekend_end IS NULL)) OR ((school_night_start >= 0) AND (school_night_end > school_night_start) AND (school_night_end < 1440) AND (weekend_start >= 0) AND (weekend_end > weekend_start) AND (weekend_end < 1440)))),
    CONSTRAINT child_color_palette CHECK ((color = ANY (ARRAY['rose'::text, 'sage'::text, 'sky'::text, 'butter'::text, 'lilac'::text, 'peach'::text, 'teal'::text, 'sand'::text]))),
    CONSTRAINT child_name_nonblank CHECK ((length(btrim(name)) > 0))
);
CREATE TABLE opencozy.chore_active_award (
    household_id uuid NOT NULL,
    occurrence_id uuid NOT NULL,
    ledger_id uuid NOT NULL
);
CREATE TABLE opencozy.chore_award_reversal (
    household_id uuid NOT NULL,
    award_id uuid NOT NULL,
    reversal_id uuid NOT NULL
);
CREATE TABLE opencozy.chore_definition (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    assigned_child_id uuid NOT NULL,
    title text NOT NULL,
    points integer NOT NULL,
    schedule_kind text NOT NULL,
    scheduled_day date,
    weekdays integer[],
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT chore_points_bound CHECK (((points >= 1) AND (points <= 1000000))),
    CONSTRAINT chore_schedule_valid CHECK ((((schedule_kind = 'once'::text) AND (scheduled_day IS NOT NULL) AND isfinite(scheduled_day) AND (weekdays IS NULL)) OR ((schedule_kind = 'daily'::text) AND (scheduled_day IS NULL) AND (weekdays IS NULL)) OR ((schedule_kind = 'weekdays'::text) AND (scheduled_day IS NULL) AND (weekdays IS NOT NULL) AND ((cardinality(weekdays) >= 1) AND (cardinality(weekdays) <= 7)) AND (array_ndims(weekdays) = 1) AND (array_position(weekdays, NULL::integer) IS NULL) AND (weekdays <@ ARRAY[1, 2, 3, 4, 5, 6, 7])))),
    CONSTRAINT chore_title_nonblank CHECK ((length(btrim(title)) > 0)),
    CONSTRAINT chore_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.chore_occurrence (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    chore_id uuid NOT NULL,
    assigned_child_id uuid NOT NULL,
    local_day date NOT NULL,
    time_zone_snapshot text NOT NULL,
    title_snapshot text NOT NULL,
    points_snapshot integer NOT NULL,
    schedule_kind_snapshot text NOT NULL,
    scheduled_day_snapshot date,
    weekdays_snapshot integer[],
    definition_version integer NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT occurrence_day_finite CHECK (isfinite(local_day)),
    CONSTRAINT occurrence_points_bound CHECK (((points_snapshot >= 1) AND (points_snapshot <= 1000000))),
    CONSTRAINT occurrence_schedule_valid CHECK ((((schedule_kind_snapshot = 'once'::text) AND (scheduled_day_snapshot IS NOT NULL) AND isfinite(scheduled_day_snapshot) AND (weekdays_snapshot IS NULL)) OR ((schedule_kind_snapshot = 'daily'::text) AND (scheduled_day_snapshot IS NULL) AND (weekdays_snapshot IS NULL)) OR ((schedule_kind_snapshot = 'weekdays'::text) AND (scheduled_day_snapshot IS NULL) AND (weekdays_snapshot IS NOT NULL) AND ((cardinality(weekdays_snapshot) >= 1) AND (cardinality(weekdays_snapshot) <= 7)) AND (array_ndims(weekdays_snapshot) = 1) AND (array_position(weekdays_snapshot, NULL::integer) IS NULL) AND (weekdays_snapshot <@ ARRAY[1, 2, 3, 4, 5, 6, 7])))),
    CONSTRAINT occurrence_title_nonblank CHECK ((length(btrim(title_snapshot)) > 0)),
    CONSTRAINT occurrence_versions_positive CHECK (((version > 0) AND (definition_version > 0))),
    CONSTRAINT occurrence_zone_nonblank CHECK ((length(btrim(time_zone_snapshot)) > 0))
);
CREATE TABLE opencozy.command (
    household_id uuid NOT NULL,
    request_id uuid NOT NULL,
    domain text NOT NULL,
    command_type text NOT NULL,
    payload jsonb NOT NULL,
    status integer NOT NULL,
    outcome jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    assistant_credential_id uuid,
    CONSTRAINT command_domain_type CHECK ((((domain = 'chore'::text) AND (command_type = ANY (ARRAY['complete'::text, 'undo'::text])) AND (status = ANY (ARRAY[200, 404, 409]))) OR ((domain = 'reward'::text) AND (command_type = ANY (ARRAY['create'::text, 'edit'::text, 'redeem'::text, 'fulfill'::text, 'cancel'::text, 'grant'::text, 'spend'::text, 'end'::text]))) OR ((domain = 'routine'::text) AND (command_type = ANY (ARRAY['create'::text, 'edit'::text, 'step'::text, 'undo'::text]))) OR ((domain = 'external'::text) AND (command_type = 'award'::text) AND (status = ANY (ARRAY[200, 201, 403, 404, 409]))) OR ((domain = 'dashboard'::text) AND (command_type = ANY (ARRAY['create'::text, 'edit'::text]))) OR ((domain = 'meal'::text) AND (command_type = ANY (ARRAY['recipe.create'::text, 'recipe.edit'::text, 'plan.set'::text, 'plan.clear'::text])) AND (status = ANY (ARRAY[200, 201, 404, 409]))) OR ((domain = 'approval'::text) AND (command_type = ANY (ARRAY['approve'::text, 'reject'::text])) AND (status = ANY (ARRAY[200, 404, 409]))) OR ((domain = 'tv'::text) AND (command_type = ANY (ARRAY['answer'::text, 'start'::text, 'end'::text, 'done'::text, 'watch'::text, 'open'::text, 'extend'::text, 'close'::text])) AND (status = ANY (ARRAY[200, 201, 404, 409]))) OR ((domain = 'time_request'::text) AND (command_type = ANY (ARRAY['ask'::text, 'answer'::text])) AND (status = ANY (ARRAY[200, 201, 404, 409]))))),
    CONSTRAINT command_outcome_object CHECK ((jsonb_typeof(outcome) = 'object'::text)),
    CONSTRAINT command_payload_object CHECK ((jsonb_typeof(payload) = 'object'::text)),
    CONSTRAINT command_status CHECK ((status = ANY (ARRAY[200, 201, 403, 404, 409])))
);
CREATE TABLE opencozy.completion_approval (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    child_id uuid NOT NULL,
    chore_occurrence_id uuid,
    routine_occurrence_id uuid,
    title_snapshot text NOT NULL,
    points_snapshot integer NOT NULL,
    local_day date NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    request_command_id uuid NOT NULL,
    device_id uuid,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    decided_at timestamp with time zone,
    decided_by text,
    reason text,
    ledger_id uuid,
    CONSTRAINT completion_approval_day_finite CHECK (isfinite(local_day)),
    CONSTRAINT completion_approval_decider CHECK (((state <> ALL (ARRAY['approved'::text, 'rejected'::text])) OR (decided_by IS NOT NULL))),
    CONSTRAINT completion_approval_decision CHECK ((((state = 'pending'::text) AND (version = 1) AND (decided_at IS NULL) AND (decided_by IS NULL)) OR ((state <> 'pending'::text) AND (version = 2) AND (decided_at IS NOT NULL) AND (decided_at >= requested_at)))),
    CONSTRAINT completion_approval_ledger CHECK (((state = 'approved'::text) = (ledger_id IS NOT NULL))),
    CONSTRAINT completion_approval_origin CHECK (((chore_occurrence_id IS NULL) <> (routine_occurrence_id IS NULL))),
    CONSTRAINT completion_approval_points_bound CHECK (((points_snapshot >= 1) AND (points_snapshot <= 1000000))),
    CONSTRAINT completion_approval_reason CHECK (((reason IS NULL) OR ((state = 'rejected'::text) AND ((length(btrim(reason)) >= 1) AND (length(btrim(reason)) <= 200))))),
    CONSTRAINT completion_approval_state CHECK ((state = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'withdrawn'::text]))),
    CONSTRAINT completion_approval_title_nonblank CHECK ((length(btrim(title_snapshot)) > 0))
);
CREATE TABLE opencozy.dashboard_view (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    name text NOT NULL,
    widgets jsonb DEFAULT '[]'::jsonb NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT dashboard_view_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 120))),
    CONSTRAINT dashboard_view_version_positive CHECK ((version > 0)),
    CONSTRAINT dashboard_view_widgets_array CHECK ((jsonb_typeof(widgets) = 'array'::text))
);
CREATE TABLE opencozy.device (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    child_id uuid,
    name text NOT NULL,
    platform text NOT NULL,
    public_key text NOT NULL,
    paired_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone,
    revoked_at timestamp with time zone,
    push_token text,
    push_environment text,
    push_registered_at timestamp with time zone,
    CONSTRAINT device_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT device_platform CHECK ((platform = ANY (ARRAY['ios'::text, 'android'::text]))),
    CONSTRAINT device_public_key_format CHECK ((public_key ~ '^B[A-P][A-Za-z0-9+/]{85}=$'::text)),
    CONSTRAINT device_push CHECK ((((push_token IS NULL) AND (push_environment IS NULL) AND (push_registered_at IS NULL)) OR ((push_token ~ '^[a-f0-9]{64,200}$'::text) AND (push_environment = ANY (ARRAY['development'::text, 'production'::text])) AND (push_registered_at IS NOT NULL) AND (revoked_at IS NULL))))
);
CREATE TABLE opencozy.device_nonce (
    device_id uuid NOT NULL,
    nonce text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT device_nonce_format CHECK ((nonce ~ '^[A-Za-z0-9_-]{16,64}$'::text))
);
CREATE TABLE opencozy.device_pairing (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    child_id uuid NOT NULL,
    token_hash text NOT NULL,
    code_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    device_id uuid,
    CONSTRAINT device_pairing_code_hash_format CHECK ((code_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT device_pairing_consumed CHECK (((consumed_at IS NULL) = (device_id IS NULL))),
    CONSTRAINT device_pairing_expiry_window CHECK ((isfinite(expires_at) AND (expires_at > created_at) AND (expires_at <= (created_at + '00:05:00'::interval)))),
    CONSTRAINT device_pairing_token_hash_format CHECK ((token_hash ~ '^[a-f0-9]{64}$'::text))
);
CREATE TABLE opencozy.device_web_session (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    device_id uuid NOT NULL,
    child_id uuid NOT NULL,
    exchange_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    exchange_expires_at timestamp with time zone NOT NULL,
    exchanged_at timestamp with time zone,
    credential_hash text,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    CONSTRAINT device_web_session_credential_hash_format CHECK (((credential_hash IS NULL) OR (credential_hash ~ '^[a-f0-9]{64}$'::text))),
    CONSTRAINT device_web_session_exchange_hash_format CHECK ((exchange_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT device_web_session_exchange_window CHECK ((isfinite(exchange_expires_at) AND (exchange_expires_at > created_at) AND (exchange_expires_at <= (created_at + '00:01:00'::interval)))),
    CONSTRAINT device_web_session_exchanged CHECK ((((exchanged_at IS NULL) = (credential_hash IS NULL)) AND ((exchanged_at IS NULL) = (expires_at IS NULL))))
);
CREATE TABLE opencozy.display_pairing (
    id uuid NOT NULL,
    household_id uuid NOT NULL,
    code_hash text NOT NULL,
    name text NOT NULL,
    scopes text[] NOT NULL,
    session_ttl_seconds integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    sound_enabled boolean DEFAULT true NOT NULL,
    CONSTRAINT display_pairing_code_hash_format CHECK ((code_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT display_pairing_expiry_window CHECK ((isfinite(expires_at) AND (expires_at > created_at) AND (expires_at <= (created_at + '00:05:00'::interval)))),
    CONSTRAINT display_pairing_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT display_pairing_scopes_valid CHECK (((array_position(scopes, NULL::text) IS NULL) AND ((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 14)) AND (scopes <@ ARRAY['children:read'::text, 'chores:read'::text, 'chores:complete'::text, 'routines:read'::text, 'routines:check'::text, 'points:read'::text, 'rewards:read'::text, 'rewards:redeem'::text, 'meals:read'::text, 'calendar:read'::text, 'home:read'::text, 'dashboard:read'::text, 'tv:request'::text, 'tv:done'::text]))),
    CONSTRAINT display_pairing_ttl_bound CHECK (((session_ttl_seconds >= 60) AND (session_ttl_seconds <= 2592000)))
);
CREATE TABLE opencozy.display_session (
    id uuid NOT NULL,
    household_id uuid NOT NULL,
    pairing_id uuid NOT NULL,
    credential_hash text NOT NULL,
    name text NOT NULL,
    scopes text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    sound_enabled boolean DEFAULT true NOT NULL,
    CONSTRAINT display_session_credential_hash_format CHECK ((credential_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT display_session_expiry_window CHECK ((isfinite(expires_at) AND (expires_at > created_at) AND (expires_at <= (created_at + '30 days'::interval)))),
    CONSTRAINT display_session_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT display_session_scopes_valid CHECK (((array_position(scopes, NULL::text) IS NULL) AND ((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 14)) AND (scopes <@ ARRAY['children:read'::text, 'chores:read'::text, 'chores:complete'::text, 'routines:read'::text, 'routines:check'::text, 'points:read'::text, 'rewards:read'::text, 'rewards:redeem'::text, 'meals:read'::text, 'calendar:read'::text, 'home:read'::text, 'dashboard:read'::text, 'tv:request'::text, 'tv:done'::text])))
);
CREATE TABLE opencozy.external_award (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    source_event_id text NOT NULL,
    child_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    rule_snapshot jsonb NOT NULL,
    ledger_id uuid NOT NULL,
    CONSTRAINT external_award_rule_snapshot_object CHECK ((jsonb_typeof(rule_snapshot) = 'object'::text)),
    CONSTRAINT external_award_source_event_id_length CHECK (((length(btrim(source_event_id)) >= 1) AND (length(btrim(source_event_id)) <= 255)))
);
CREATE TABLE opencozy.external_identity_link (
    household_id uuid NOT NULL,
    child_id uuid NOT NULL,
    provider text NOT NULL,
    external_id text NOT NULL,
    linked_at timestamp with time zone DEFAULT now() NOT NULL,
    unlinked_at timestamp with time zone,
    CONSTRAINT external_identity_link_external_id_length CHECK (((length(btrim(external_id)) >= 1) AND (length(btrim(external_id)) <= 255))),
    CONSTRAINT external_identity_link_provider_length CHECK (((length(btrim(provider)) >= 1) AND (length(btrim(provider)) <= 80))),
    CONSTRAINT external_identity_link_unlink_order CHECK (((unlinked_at IS NULL) OR (unlinked_at >= linked_at)))
);
CREATE TABLE opencozy.gated_tv (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    room text,
    ha_entity_id text,
    power text DEFAULT 'unknown'::text NOT NULL,
    power_changed_at timestamp with time zone,
    attempts_since timestamp with time zone,
    last_attempt_at timestamp with time zone,
    open_start integer,
    open_end integer,
    open_until timestamp with time zone,
    open_days smallint,
    CONSTRAINT gated_tv_attempts CHECK ((((attempts_since IS NULL) = (last_attempt_at IS NULL)) AND ((attempts_since IS NULL) OR (attempts_since <= last_attempt_at)))),
    CONSTRAINT gated_tv_entity_format CHECK (((ha_entity_id IS NULL) OR (ha_entity_id ~ '^[a-z0-9_]{1,64}[.][a-z0-9_]{1,190}$'::text))),
    CONSTRAINT gated_tv_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT gated_tv_open_days CHECK (((open_days IS NULL) OR ((open_start IS NOT NULL) AND ((open_days >= 1) AND (open_days <= 126))))),
    CONSTRAINT gated_tv_open_hours CHECK ((((open_start IS NULL) = (open_end IS NULL)) AND ((open_start IS NULL) OR (((open_start >= 0) AND (open_start <= 1439)) AND ((open_end >= 0) AND (open_end <= 1439)) AND (open_start <> open_end))))),
    CONSTRAINT gated_tv_open_until_finite CHECK (((open_until IS NULL) OR isfinite(open_until))),
    CONSTRAINT gated_tv_power CHECK ((power = ANY (ARRAY['unknown'::text, 'on'::text, 'off'::text]))),
    CONSTRAINT gated_tv_room_length CHECK (((room IS NULL) OR ((length(btrim(room)) >= 1) AND (length(btrim(room)) <= 80))))
);
CREATE TABLE opencozy.gated_tv_mac (
    household_id uuid NOT NULL,
    tv_id uuid NOT NULL,
    mac text NOT NULL,
    CONSTRAINT gated_tv_mac_mac_format CHECK ((mac ~ '^[0-9a-f]{2}(:[0-9a-f]{2}){5}$'::text))
);
CREATE TABLE opencozy.home_assistant_connection (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    base_url text NOT NULL,
    token_ciphertext text NOT NULL,
    status text DEFAULT 'unverified'::text NOT NULL,
    last_checked_at timestamp with time zone,
    last_error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT home_assistant_connection_base_url_length CHECK (((length(btrim(base_url)) >= 1) AND (length(btrim(base_url)) <= 2048))),
    CONSTRAINT home_assistant_connection_error_length CHECK (((last_error_message IS NULL) OR (length(last_error_message) <= 500))),
    CONSTRAINT home_assistant_connection_status_valid CHECK ((status = ANY (ARRAY['unverified'::text, 'verified'::text, 'error'::text]))),
    CONSTRAINT home_assistant_connection_token_format CHECK ((token_ciphertext ~ '^[a-f0-9]+:[a-f0-9]+:[a-f0-9]+$'::text))
);
CREATE TABLE opencozy.home_assistant_entity (
    household_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    entity_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    fetched_at timestamp with time zone,
    status text,
    error_message text,
    state text,
    attributes jsonb,
    last_success_at timestamp with time zone,
    last_success_state text,
    last_success_attributes jsonb,
    CONSTRAINT home_assistant_entity_error_length CHECK (((error_message IS NULL) OR (length(error_message) <= 500))),
    CONSTRAINT home_assistant_entity_id_format CHECK ((entity_id ~ '^[a-z_]+\.[a-z0-9_]+$'::text)),
    CONSTRAINT home_assistant_entity_id_length CHECK (((length(btrim(entity_id)) >= 1) AND (length(btrim(entity_id)) <= 255))),
    CONSTRAINT home_assistant_entity_last_success_state_length CHECK (((last_success_state IS NULL) OR (length(last_success_state) <= 255))),
    CONSTRAINT home_assistant_entity_state_length CHECK (((state IS NULL) OR (length(state) <= 255))),
    CONSTRAINT home_assistant_entity_status_valid CHECK (((status IS NULL) OR (status = ANY (ARRAY['ok'::text, 'error'::text]))))
);
CREATE TABLE opencozy.household (
    id uuid NOT NULL,
    name text NOT NULL,
    time_zone text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    lock_message text,
    game_portal_domains text[] DEFAULT '{}'::text[] NOT NULL,
    game_domains text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT household_game_domains_valid CHECK (((array_position(game_domains, NULL::text) IS NULL) AND (cardinality(game_domains) <= 500) AND (array_to_string(game_domains, ' '::text) ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+( [a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+)*)?$'::text))),
    CONSTRAINT household_game_portal_domains_valid CHECK (((array_position(game_portal_domains, NULL::text) IS NULL) AND (cardinality(game_portal_domains) <= 500) AND (array_to_string(game_portal_domains, ' '::text) ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+( [a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+)*)?$'::text))),
    CONSTRAINT household_lock_message_length CHECK (((lock_message IS NULL) OR ((length(btrim(lock_message)) >= 1) AND (length(btrim(lock_message)) <= 200)))),
    CONSTRAINT household_name_nonblank CHECK ((length(btrim(name)) > 0)),
    CONSTRAINT household_time_zone_nonblank CHECK ((length(btrim(time_zone)) > 0))
);
CREATE TABLE opencozy.household_owner (
    user_id text NOT NULL,
    household_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE opencozy.icloud_calendar (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    calendar_href text NOT NULL,
    name text NOT NULL,
    remote_display_name text,
    color text,
    show_on_display boolean DEFAULT true NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_attempt_at timestamp with time zone,
    last_success_at timestamp with time zone,
    last_changed_at timestamp with time zone,
    last_error_at timestamp with time zone,
    last_error_code text,
    consecutive_failures integer DEFAULT 0 NOT NULL,
    skipped_components integer DEFAULT 0 NOT NULL,
    refresh_lease_id uuid,
    refresh_lease_until timestamp with time zone,
    ctag text,
    sync_token text,
    applied_window_key text,
    CONSTRAINT icloud_calendar_cache CHECK ((((ctag IS NULL) OR ((length(ctag) >= 1) AND (length(ctag) <= 1024))) AND ((sync_token IS NULL) OR ((length(sync_token) >= 1) AND (length(sync_token) <= 4096))) AND ((applied_window_key IS NULL) OR ((length(applied_window_key) >= 1) AND (length(applied_window_key) <= 200))))),
    CONSTRAINT icloud_calendar_counters CHECK (((consecutive_failures >= 0) AND (skipped_components >= 0))),
    CONSTRAINT icloud_calendar_error CHECK ((((last_error_code IS NULL) = (last_error_at IS NULL)) AND ((last_error_code IS NULL) OR (last_error_code = ANY (ARRAY['invalid_credentials'::text, 'forbidden'::text, 'not_found'::text, 'blocked_address'::text, 'dns_failed'::text, 'connect_failed'::text, 'tls_failed'::text, 'timeout'::text, 'too_large'::text, 'redirect_blocked'::text, 'too_many_redirects'::text, 'server_error'::text, 'http_error'::text, 'invalid_response'::text, 'sync_token_invalid'::text, 'too_many_events'::text, 'internal'::text]))))),
    CONSTRAINT icloud_calendar_href_length CHECK (((length(calendar_href) >= 1) AND (length(calendar_href) <= 2048))),
    CONSTRAINT icloud_calendar_lease CHECK (((refresh_lease_id IS NULL) = (refresh_lease_until IS NULL))),
    CONSTRAINT icloud_calendar_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT icloud_calendar_remote_name_length CHECK (((remote_display_name IS NULL) OR ((length(remote_display_name) >= 1) AND (length(remote_display_name) <= 200)))),
    CONSTRAINT icloud_calendar_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.icloud_connection (
    household_id uuid NOT NULL,
    apple_id text NOT NULL,
    credential_ciphertext text NOT NULL,
    credential_updated_at timestamp with time zone DEFAULT now() NOT NULL,
    principal_href text,
    home_href text,
    status text DEFAULT 'connected'::text NOT NULL,
    last_validated_at timestamp with time zone,
    last_error_at timestamp with time zone,
    last_error_code text,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT icloud_connection_apple_id_length CHECK (((length(btrim(apple_id)) >= 1) AND (length(btrim(apple_id)) <= 254))),
    CONSTRAINT icloud_connection_error CHECK ((((last_error_code IS NULL) = (last_error_at IS NULL)) AND ((last_error_code IS NULL) OR (last_error_code = ANY (ARRAY['invalid_credentials'::text, 'forbidden'::text, 'not_found'::text, 'blocked_address'::text, 'dns_failed'::text, 'connect_failed'::text, 'tls_failed'::text, 'timeout'::text, 'too_large'::text, 'redirect_blocked'::text, 'too_many_redirects'::text, 'server_error'::text, 'http_error'::text, 'invalid_response'::text, 'sync_token_invalid'::text, 'too_many_events'::text, 'internal'::text]))))),
    CONSTRAINT icloud_connection_hrefs CHECK ((((principal_href IS NULL) OR ((length(principal_href) >= 1) AND (length(principal_href) <= 2048))) AND ((home_href IS NULL) OR ((length(home_href) >= 1) AND (length(home_href) <= 2048))))),
    CONSTRAINT icloud_connection_status CHECK ((status = ANY (ARRAY['connected'::text, 'error'::text, 'disconnected'::text]))),
    CONSTRAINT icloud_connection_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.icloud_occurrence (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    calendar_id uuid NOT NULL,
    series_id uuid NOT NULL,
    recurrence_key text NOT NULL,
    all_day boolean NOT NULL,
    starts_at timestamp with time zone,
    ends_at timestamp with time zone,
    start_date date,
    end_date date,
    time_zone text,
    summary text,
    location text,
    description text,
    status text NOT NULL,
    is_override boolean NOT NULL,
    revision integer DEFAULT 1 NOT NULL,
    content_hash text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT icloud_occurrence_content_hash CHECK ((content_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT icloud_occurrence_key_format CHECK ((recurrence_key ~ '^(single|[0-9]{8}|[0-9]{8}T[0-9]{6}Z)$'::text)),
    CONSTRAINT icloud_occurrence_revision_positive CHECK ((revision > 0)),
    CONSTRAINT icloud_occurrence_status CHECK ((status = ANY (ARRAY['confirmed'::text, 'tentative'::text, 'cancelled'::text]))),
    CONSTRAINT icloud_occurrence_text_length CHECK ((((summary IS NULL) OR ((length(btrim(summary)) >= 1) AND (length(btrim(summary)) <= 500))) AND ((location IS NULL) OR ((length(btrim(location)) >= 1) AND (length(btrim(location)) <= 500))) AND ((description IS NULL) OR ((length(btrim(description)) >= 1) AND (length(btrim(description)) <= 4000))))),
    CONSTRAINT icloud_occurrence_time_shape CHECK (((all_day AND (start_date IS NOT NULL) AND (end_date IS NOT NULL) AND isfinite(start_date) AND isfinite(end_date) AND (end_date > start_date) AND (starts_at IS NULL) AND (ends_at IS NULL)) OR ((NOT all_day) AND (starts_at IS NOT NULL) AND (ends_at IS NOT NULL) AND isfinite(starts_at) AND isfinite(ends_at) AND (ends_at >= starts_at) AND (start_date IS NULL) AND (end_date IS NULL)))),
    CONSTRAINT icloud_occurrence_time_zone CHECK (((time_zone IS NULL) OR ((length(time_zone) >= 1) AND (length(time_zone) <= 100))))
);
CREATE TABLE opencozy.icloud_resource (
    household_id uuid NOT NULL,
    calendar_id uuid NOT NULL,
    resource_href text NOT NULL,
    etag text,
    calendar_data text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT icloud_resource_data_length CHECK (((octet_length(calendar_data) >= 1) AND (octet_length(calendar_data) <= 8388608))),
    CONSTRAINT icloud_resource_href_length CHECK (((length(resource_href) >= 1) AND (length(resource_href) <= 2048)))
);
CREATE TABLE opencozy.icloud_series (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    calendar_id uuid NOT NULL,
    provider_uid text NOT NULL,
    resource_href text NOT NULL,
    summary text,
    all_day boolean NOT NULL,
    time_zone text,
    recurring boolean NOT NULL,
    status text NOT NULL,
    revision integer DEFAULT 1 NOT NULL,
    content_hash text NOT NULL,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    removed_at timestamp with time zone,
    CONSTRAINT icloud_series_content_hash CHECK ((content_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT icloud_series_resource_href_length CHECK (((length(resource_href) >= 1) AND (length(resource_href) <= 2048))),
    CONSTRAINT icloud_series_revision_positive CHECK ((revision > 0)),
    CONSTRAINT icloud_series_status CHECK ((status = ANY (ARRAY['confirmed'::text, 'tentative'::text, 'cancelled'::text]))),
    CONSTRAINT icloud_series_summary_length CHECK (((summary IS NULL) OR ((length(btrim(summary)) >= 1) AND (length(btrim(summary)) <= 500)))),
    CONSTRAINT icloud_series_time_zone CHECK (((time_zone IS NULL) OR ((length(time_zone) >= 1) AND (length(time_zone) <= 100)))),
    CONSTRAINT icloud_series_uid_length CHECK (((length(provider_uid) >= 1) AND (length(provider_uid) <= 1024)))
);
CREATE TABLE opencozy.meal_plan_entry (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    local_day date NOT NULL,
    meal_type text NOT NULL,
    recipe_id uuid NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT meal_plan_entry_day_finite CHECK (isfinite(local_day)),
    CONSTRAINT meal_plan_entry_meal_type_valid CHECK ((meal_type = ANY (ARRAY['breakfast'::text, 'lunch'::text, 'dinner'::text]))),
    CONSTRAINT meal_plan_entry_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.owner_bootstrap (
    singleton integer NOT NULL,
    user_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT owner_bootstrap_singleton CHECK ((singleton = 1))
);
CREATE TABLE opencozy.parent_passkey (
    id uuid NOT NULL,
    user_id text NOT NULL,
    name text NOT NULL,
    credential_id text NOT NULL,
    public_key text NOT NULL,
    counter integer DEFAULT 0 NOT NULL,
    transports text[] DEFAULT ARRAY[]::text[] NOT NULL,
    backed_up boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    CONSTRAINT parent_passkey_counter CHECK ((counter >= 0)),
    CONSTRAINT parent_passkey_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80)))
);
CREATE TABLE opencozy.parent_session (
    id uuid NOT NULL,
    household_id uuid NOT NULL,
    user_id text NOT NULL,
    name text NOT NULL,
    credential_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    last_used_at timestamp with time zone,
    revoked_at timestamp with time zone,
    push_token text,
    push_environment text,
    push_registered_at timestamp with time zone,
    web_session_id text,
    CONSTRAINT parent_session_credential_hash_format CHECK ((credential_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT parent_session_expiry_window CHECK ((isfinite(expires_at) AND (expires_at > created_at) AND (expires_at <= (created_at + '90 days'::interval)))),
    CONSTRAINT parent_session_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 80))),
    CONSTRAINT parent_session_push CHECK ((((push_token IS NULL) AND (push_environment IS NULL) AND (push_registered_at IS NULL)) OR ((push_token ~ '^[a-f0-9]{64,200}$'::text) AND (push_environment = ANY (ARRAY['development'::text, 'production'::text])) AND (push_registered_at IS NOT NULL) AND (revoked_at IS NULL))))
);
CREATE TABLE opencozy.personal_credit (
    household_id uuid NOT NULL,
    redemption_id uuid NOT NULL,
    child_id uuid NOT NULL,
    target text DEFAULT 'personal'::text NOT NULL,
    minutes integer NOT NULL,
    seconds integer NOT NULL,
    used_seconds integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT personal_credit_created_finite CHECK (isfinite(created_at)),
    CONSTRAINT personal_credit_seconds CHECK (((used_seconds >= 0) AND (used_seconds <= seconds) AND (seconds <= (minutes * 60)))),
    CONSTRAINT personal_credit_target CHECK ((target = 'personal'::text))
);
CREATE TABLE opencozy.point_ledger (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    child_id uuid NOT NULL,
    occurrence_id uuid,
    occurrence_version integer,
    redemption_id uuid,
    reward_entry_type text,
    routine_occurrence_id uuid,
    routine_entry_type text,
    command_id uuid NOT NULL,
    delta integer NOT NULL,
    description_snapshot text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    external_award_id uuid,
    external_entry_type text,
    CONSTRAINT ledger_delta_bound CHECK (((delta <> 0) AND ((delta >= '-1000000'::integer) AND (delta <= 1000000)))),
    CONSTRAINT ledger_description_nonblank CHECK ((length(btrim(description_snapshot)) > 0)),
    CONSTRAINT ledger_domain_origin CHECK ((((occurrence_id IS NOT NULL) AND (occurrence_version IS NOT NULL) AND (redemption_id IS NULL) AND (reward_entry_type IS NULL) AND (routine_occurrence_id IS NULL) AND (routine_entry_type IS NULL) AND (external_award_id IS NULL) AND (external_entry_type IS NULL)) OR ((occurrence_id IS NULL) AND (occurrence_version IS NULL) AND (redemption_id IS NOT NULL) AND (reward_entry_type IS NOT NULL) AND (routine_occurrence_id IS NULL) AND (routine_entry_type IS NULL) AND (external_award_id IS NULL) AND (external_entry_type IS NULL) AND (((reward_entry_type = 'spend'::text) AND (delta < 0)) OR ((reward_entry_type = 'refund'::text) AND (delta > 0)))) OR ((occurrence_id IS NULL) AND (occurrence_version IS NULL) AND (redemption_id IS NULL) AND (reward_entry_type IS NULL) AND (routine_occurrence_id IS NOT NULL) AND (routine_entry_type IS NOT NULL) AND (external_award_id IS NULL) AND (external_entry_type IS NULL) AND (((routine_entry_type = 'award'::text) AND (delta > 0)) OR ((routine_entry_type = 'reversal'::text) AND (delta < 0)))) OR ((occurrence_id IS NULL) AND (occurrence_version IS NULL) AND (redemption_id IS NULL) AND (reward_entry_type IS NULL) AND (routine_occurrence_id IS NULL) AND (routine_entry_type IS NULL) AND (external_award_id IS NOT NULL) AND (external_entry_type IS NOT NULL) AND (((external_entry_type = 'award'::text) AND (delta > 0)) OR ((external_entry_type = 'reversal'::text) AND (delta < 0)))))),
    CONSTRAINT ledger_version_positive CHECK ((occurrence_version > 0))
);
CREATE TABLE opencozy.recipe (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    title text NOT NULL,
    servings integer NOT NULL,
    ingredients text[] NOT NULL,
    instructions text[] NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT recipe_ingredients_valid CHECK (opencozy.text_array_valid(ingredients, 40, 200)),
    CONSTRAINT recipe_instructions_valid CHECK (opencozy.text_array_valid(instructions, 40, 2000)),
    CONSTRAINT recipe_servings_bound CHECK (((servings >= 1) AND (servings <= 100))),
    CONSTRAINT recipe_title_length CHECK (((length(btrim(title)) >= 1) AND (length(btrim(title)) <= 160))),
    CONSTRAINT recipe_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.reward (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    name text NOT NULL,
    cost integer NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    screen_time_minutes integer,
    screen_time_target text,
    CONSTRAINT reward_cost_bound CHECK (((cost >= 1) AND (cost <= 1000000))),
    CONSTRAINT reward_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 160))),
    CONSTRAINT reward_screen_time CHECK ((((screen_time_minutes IS NULL) AND (screen_time_target IS NULL)) OR (((screen_time_minutes >= 1) AND (screen_time_minutes <= 240)) AND (screen_time_target = ANY (ARRAY['personal'::text, 'tv'::text]))))),
    CONSTRAINT reward_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.reward_redemption (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    reward_id uuid,
    child_id uuid NOT NULL,
    name_snapshot text NOT NULL,
    cost_snapshot integer NOT NULL,
    reward_version integer,
    version integer DEFAULT 1 NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    settled_at timestamp with time zone,
    screen_time_minutes_snapshot integer,
    screen_time_target_snapshot text,
    fulfilled_at timestamp with time zone,
    refunded_points integer,
    device_id uuid,
    CONSTRAINT redemption_cost_bound CHECK ((((reward_id IS NOT NULL) AND ((cost_snapshot >= 1) AND (cost_snapshot <= 1000000))) OR ((reward_id IS NULL) AND (cost_snapshot = 0)))),
    CONSTRAINT redemption_fulfillment CHECK (((((state = 'pending'::text) AND (fulfilled_at IS NULL)) OR ((state = 'fulfilled'::text) AND (fulfilled_at IS NOT NULL)) OR ((state = 'canceled'::text) AND ((fulfilled_at IS NULL) = (screen_time_minutes_snapshot IS NULL)))) AND ((fulfilled_at IS NULL) OR (fulfilled_at <= settled_at)))),
    CONSTRAINT redemption_name_length CHECK (((length(btrim(name_snapshot)) >= 1) AND (length(btrim(name_snapshot)) <= 160))),
    CONSTRAINT redemption_parent_grant CHECK ((((reward_id IS NULL) = (reward_version IS NULL)) AND ((reward_id IS NOT NULL) OR ((screen_time_minutes_snapshot IS NOT NULL) AND (state <> 'pending'::text))))),
    CONSTRAINT redemption_refund CHECK ((((state = 'canceled'::text) = (refunded_points IS NOT NULL)) AND ((refunded_points IS NULL) OR ((refunded_points >= 0) AND (refunded_points <= cost_snapshot))))),
    CONSTRAINT redemption_screen_time CHECK ((((screen_time_minutes_snapshot IS NULL) AND (screen_time_target_snapshot IS NULL)) OR (((screen_time_minutes_snapshot >= 1) AND (screen_time_minutes_snapshot <= 240)) AND (screen_time_target_snapshot = ANY (ARRAY['personal'::text, 'tv'::text]))) OR ((reward_id IS NULL) AND (screen_time_target_snapshot = 'personal'::text) AND ((screen_time_minutes_snapshot >= 1) AND (screen_time_minutes_snapshot <= 1440))))),
    CONSTRAINT redemption_screen_time_not_pending CHECK (((screen_time_minutes_snapshot IS NULL) OR (state <> 'pending'::text))),
    CONSTRAINT redemption_settlement CHECK ((((state = 'pending'::text) AND (settled_at IS NULL)) OR ((state <> 'pending'::text) AND (settled_at IS NOT NULL)))),
    CONSTRAINT redemption_state CHECK ((state = ANY (ARRAY['pending'::text, 'fulfilled'::text, 'canceled'::text]))),
    CONSTRAINT redemption_versions_positive CHECK (((version > 0) AND ((reward_version IS NULL) OR (reward_version > 0))))
);
CREATE TABLE opencozy.routine_definition (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    assigned_child_id uuid NOT NULL,
    title text NOT NULL,
    points integer NOT NULL,
    steps jsonb NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    CONSTRAINT routine_points_bound CHECK (((points >= 1) AND (points <= 1000000))),
    CONSTRAINT routine_steps_shape CHECK (opencozy.routine_steps_valid(steps)),
    CONSTRAINT routine_title_length CHECK (((length(btrim(title)) >= 1) AND (length(btrim(title)) <= 160))),
    CONSTRAINT routine_version_positive CHECK ((version > 0))
);
CREATE TABLE opencozy.routine_occurrence (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    routine_id uuid NOT NULL,
    assigned_child_id uuid NOT NULL,
    local_day date NOT NULL,
    time_zone_snapshot text NOT NULL,
    title_snapshot text NOT NULL,
    points_snapshot integer NOT NULL,
    steps_snapshot jsonb NOT NULL,
    checked_step_ids uuid[] DEFAULT ARRAY[]::uuid[] NOT NULL,
    definition_version integer NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT routine_occurrence_day_finite CHECK (isfinite(local_day)),
    CONSTRAINT routine_occurrence_points_bound CHECK (((points_snapshot >= 1) AND (points_snapshot <= 1000000))),
    CONSTRAINT routine_occurrence_steps_shape CHECK (opencozy.routine_steps_valid(steps_snapshot)),
    CONSTRAINT routine_occurrence_title_length CHECK (((length(btrim(title_snapshot)) >= 1) AND (length(btrim(title_snapshot)) <= 160))),
    CONSTRAINT routine_occurrence_versions_positive CHECK (((version > 0) AND (definition_version > 0))),
    CONSTRAINT routine_occurrence_zone_nonblank CHECK ((length(btrim(time_zone_snapshot)) > 0))
);
CREATE TABLE opencozy.schema_migrations (
    version text NOT NULL,
    checksum text NOT NULL,
    applied_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE opencozy.screen_time_event (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    child_id uuid NOT NULL,
    device_id uuid NOT NULL,
    kind text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT screen_time_event_kind CHECK ((kind = 'one_more_minute'::text)),
    CONSTRAINT screen_time_event_time CHECK ((occurred_at <= created_at))
);
CREATE TABLE opencozy.screen_time_grant (
    household_id uuid NOT NULL,
    redemption_id uuid NOT NULL,
    child_id uuid NOT NULL,
    target text NOT NULL,
    minutes integer NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    CONSTRAINT screen_time_grant_personal CHECK ((target = 'personal'::text)),
    CONSTRAINT screen_time_grant_window CHECK ((isfinite(starts_at) AND isfinite(ends_at) AND (ends_at >= starts_at) AND (ends_at <= (starts_at + make_interval(mins => minutes)))))
);
CREATE TABLE opencozy.screen_time_grant_cut (
    household_id uuid NOT NULL,
    grant_id uuid NOT NULL,
    child_id uuid NOT NULL,
    cut_at timestamp with time zone NOT NULL,
    refunded_seconds integer NOT NULL,
    cut_by text,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT screen_time_grant_cut_values CHECK ((isfinite(cut_at) AND isfinite(created_at) AND (refunded_seconds >= 0)))
);
CREATE TABLE opencozy.service_credential (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    name text NOT NULL,
    credential_hash text NOT NULL,
    scopes text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT service_credential_hash_format CHECK ((credential_hash ~ '^[a-f0-9]{64}$'::text)),
    CONSTRAINT service_credential_name_length CHECK (((length(btrim(name)) >= 1) AND (length(btrim(name)) <= 160))),
    CONSTRAINT service_credential_scopes_valid CHECK (((array_position(scopes, NULL::text) IS NULL) AND ((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 16)) AND (scopes <@ ARRAY['children:read'::text, 'chores:read'::text, 'chores:complete'::text, 'routines:read'::text, 'routines:check'::text, 'points:read'::text, 'history:read'::text, 'rewards:read'::text, 'rewards:redeem'::text, 'screentime:read'::text, 'calendar:read'::text, 'external:award'::text, 'tv:gate'::text, 'tv:report'::text, 'tv:done'::text, 'games:gate'::text])))
);
CREATE TABLE opencozy.service_credential_award_rule (
    household_id uuid NOT NULL,
    credential_id uuid NOT NULL,
    rule_id uuid NOT NULL
);
CREATE TABLE opencozy.service_credential_child (
    household_id uuid NOT NULL,
    credential_id uuid NOT NULL,
    child_id uuid NOT NULL
);
CREATE TABLE opencozy.time_request (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    child_id uuid NOT NULL,
    device_id uuid NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    decided_by text,
    choice text,
    redemption_id uuid,
    granted_until timestamp with time zone,
    CONSTRAINT time_request_choice CHECK (((choice IS NULL) OR (choice = ANY (ARRAY['30min'::text, '1h'::text, 'all_day'::text, 'no'::text])))),
    CONSTRAINT time_request_decision CHECK ((((state = 'pending'::text) AND (version = 1) AND (decided_at IS NULL) AND (decided_by IS NULL) AND (choice IS NULL)) OR ((state <> 'pending'::text) AND (version = 2) AND (decided_at >= created_at) AND (decided_by IS NOT NULL) AND (choice IS NOT NULL)))),
    CONSTRAINT time_request_outcome CHECK ((((state = 'approved'::text) = ((redemption_id IS NOT NULL) AND (granted_until IS NOT NULL))) AND ((state <> 'approved'::text) OR (choice <> 'no'::text)) AND ((state <> 'declined'::text) OR (choice = 'no'::text)))),
    CONSTRAINT time_request_state CHECK ((state = ANY (ARRAY['pending'::text, 'approved'::text, 'declined'::text])))
);
CREATE TABLE opencozy.tv_credit (
    household_id uuid NOT NULL,
    redemption_id uuid NOT NULL,
    child_id uuid NOT NULL,
    target text DEFAULT 'tv'::text NOT NULL,
    minutes integer NOT NULL,
    seconds integer NOT NULL,
    used_seconds integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT tv_credit_created_finite CHECK (isfinite(created_at)),
    CONSTRAINT tv_credit_seconds CHECK (((used_seconds >= 0) AND (used_seconds <= seconds) AND (seconds <= (minutes * 60)))),
    CONSTRAINT tv_credit_target CHECK ((target = 'tv'::text))
);
CREATE TABLE opencozy.tv_open_event (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    tv_id uuid NOT NULL,
    action text NOT NULL,
    until timestamp with time zone,
    at timestamp with time zone NOT NULL,
    by text NOT NULL,
    assistant_credential_id uuid,
    ended_session_id uuid,
    CONSTRAINT tv_open_event_action CHECK ((action = ANY (ARRAY['open'::text, 'extend'::text, 'close'::text]))),
    CONSTRAINT tv_open_event_at_finite CHECK (isfinite(at)),
    CONSTRAINT tv_open_event_session CHECK (((ended_session_id IS NULL) OR (action = 'open'::text))),
    CONSTRAINT tv_open_event_until CHECK ((((action = 'close'::text) = (until IS NULL)) AND ((until IS NULL) OR (isfinite(until) AND (until > at)))))
);
CREATE TABLE opencozy.tv_prompt (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    tv_id uuid NOT NULL,
    source text NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    decided_by text,
    session_id uuid,
    CONSTRAINT tv_prompt_answer CHECK ((((state = 'answered'::text) = (session_id IS NOT NULL)) AND ((state <> ALL (ARRAY['answered'::text, 'dismissed'::text])) OR (decided_by IS NOT NULL)))),
    CONSTRAINT tv_prompt_decision CHECK ((((state = 'pending'::text) AND (version = 1) AND (decided_at IS NULL) AND (decided_by IS NULL)) OR ((state <> 'pending'::text) AND (version = 2) AND (decided_at IS NOT NULL) AND (decided_at >= created_at)))),
    CONSTRAINT tv_prompt_source CHECK ((source = ANY (ARRAY['home_assistant'::text, 'router'::text, 'display'::text, 'adult'::text]))),
    CONSTRAINT tv_prompt_state CHECK ((state = ANY (ARRAY['pending'::text, 'answered'::text, 'dismissed'::text, 'withdrawn'::text])))
);
CREATE TABLE opencozy.tv_session (
    household_id uuid NOT NULL,
    id uuid NOT NULL,
    tv_id uuid NOT NULL,
    watcher text NOT NULL,
    child_id uuid,
    charged_seconds integer DEFAULT 0 NOT NULL,
    started_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    end_reason text,
    started_by text NOT NULL,
    ended_by text,
    ends_by timestamp with time zone,
    warned_at timestamp with time zone,
    CONSTRAINT tv_session_charged CHECK (((charged_seconds >= 0) AND ((watcher = 'child'::text) OR (charged_seconds = 0)))),
    CONSTRAINT tv_session_end CHECK ((((ended_at IS NULL) = (end_reason IS NULL)) AND ((ended_at IS NULL) OR (ended_at >= started_at)) AND ((end_reason IS NULL) OR (end_reason = ANY (ARRAY['tv_off'::text, 'time_up'::text, 'parent_ended'::text, 'done'::text, 'open_hours'::text, 'parent_override'::text]))))),
    CONSTRAINT tv_session_ends_by CHECK (((ends_by IS NULL) OR ((watcher = 'child'::text) AND isfinite(ends_by) AND (ends_by > started_at)))),
    CONSTRAINT tv_session_times_finite CHECK ((isfinite(started_at) AND ((ended_at IS NULL) OR isfinite(ended_at)))),
    CONSTRAINT tv_session_watcher CHECK ((((watcher = 'parents'::text) AND (child_id IS NULL)) OR ((watcher = 'child'::text) AND (child_id IS NOT NULL))))
);
CREATE TABLE opencozy.webauthn_challenge (
    id uuid NOT NULL,
    purpose text NOT NULL,
    user_id text,
    challenge text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT webauthn_challenge_expiry CHECK (((expires_at > created_at) AND (expires_at <= (created_at + '00:05:00'::interval)))),
    CONSTRAINT webauthn_challenge_purpose CHECK ((((purpose = 'register'::text) AND (user_id IS NOT NULL)) OR ((purpose = 'authenticate'::text) AND (user_id IS NULL))))
);
ALTER TABLE ONLY opencozy.assistant_credential
    ADD CONSTRAINT assistant_credential_credential_hash_unique UNIQUE (credential_hash);
ALTER TABLE ONLY opencozy.assistant_credential
    ADD CONSTRAINT assistant_credential_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.auth_account
    ADD CONSTRAINT auth_account_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.auth_account
    ADD CONSTRAINT auth_account_provider_id_account_id_unique UNIQUE (provider_id, account_id);
ALTER TABLE ONLY opencozy.auth_session
    ADD CONSTRAINT auth_session_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.auth_session
    ADD CONSTRAINT auth_session_token_unique UNIQUE (token);
ALTER TABLE ONLY opencozy.auth_user
    ADD CONSTRAINT auth_user_email_unique UNIQUE (email);
ALTER TABLE ONLY opencozy.auth_user
    ADD CONSTRAINT auth_user_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.auth_verification
    ADD CONSTRAINT auth_verification_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.award_rule
    ADD CONSTRAINT award_rule_household_id_id_activity_key_unique UNIQUE (household_id, id, activity_key);
ALTER TABLE ONLY opencozy.award_rule
    ADD CONSTRAINT award_rule_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.calendar_assignment
    ADD CONSTRAINT calendar_assignment_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.calendar_assignment_person
    ADD CONSTRAINT calendar_assignment_person_pk PRIMARY KEY (household_id, assignment_id, child_id);
ALTER TABLE ONLY opencozy.calendar_google_connection
    ADD CONSTRAINT calendar_google_connection_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.calendar_google_connection
    ADD CONSTRAINT calendar_google_connection_subject_unique UNIQUE (household_id, google_subject);
ALTER TABLE ONLY opencozy.calendar_occurrence
    ADD CONSTRAINT calendar_occurrence_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.calendar_occurrence
    ADD CONSTRAINT calendar_occurrence_key_unique UNIQUE (household_id, series_id, recurrence_key);
ALTER TABLE ONLY opencozy.calendar_series
    ADD CONSTRAINT calendar_series_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.calendar_series
    ADD CONSTRAINT calendar_series_provider_uid_unique UNIQUE (household_id, source_id, provider_uid);
ALTER TABLE ONLY opencozy.calendar_series
    ADD CONSTRAINT calendar_series_source_unique UNIQUE (household_id, source_id, id);
ALTER TABLE ONLY opencozy.calendar_source
    ADD CONSTRAINT calendar_source_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.child
    ADD CONSTRAINT child_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.chore_active_award
    ADD CONSTRAINT chore_active_award_household_id_ledger_id_unique UNIQUE (household_id, ledger_id);
ALTER TABLE ONLY opencozy.chore_active_award
    ADD CONSTRAINT chore_active_award_household_id_occurrence_id_pk PRIMARY KEY (household_id, occurrence_id);
ALTER TABLE ONLY opencozy.chore_award_reversal
    ADD CONSTRAINT chore_award_reversal_household_id_award_id_pk PRIMARY KEY (household_id, award_id);
ALTER TABLE ONLY opencozy.chore_award_reversal
    ADD CONSTRAINT chore_award_reversal_household_id_reversal_id_unique UNIQUE (household_id, reversal_id);
ALTER TABLE ONLY opencozy.chore_definition
    ADD CONSTRAINT chore_definition_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.chore_occurrence
    ADD CONSTRAINT chore_occurrence_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.command
    ADD CONSTRAINT command_household_id_request_id_pk PRIMARY KEY (household_id, request_id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_ledger_unique UNIQUE (household_id, ledger_id);
ALTER TABLE ONLY opencozy.dashboard_view
    ADD CONSTRAINT dashboard_view_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.device
    ADD CONSTRAINT device_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.device
    ADD CONSTRAINT device_id_unique UNIQUE (id);
ALTER TABLE ONLY opencozy.device_nonce
    ADD CONSTRAINT device_nonce_device_id_nonce_pk PRIMARY KEY (device_id, nonce);
ALTER TABLE ONLY opencozy.device_pairing
    ADD CONSTRAINT device_pairing_code_hash_unique UNIQUE (code_hash);
ALTER TABLE ONLY opencozy.device_pairing
    ADD CONSTRAINT device_pairing_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.device_pairing
    ADD CONSTRAINT device_pairing_token_hash_unique UNIQUE (token_hash);
ALTER TABLE ONLY opencozy.device
    ADD CONSTRAINT device_public_key_unique UNIQUE (public_key);
ALTER TABLE ONLY opencozy.device_web_session
    ADD CONSTRAINT device_web_session_credential_hash_unique UNIQUE (credential_hash);
ALTER TABLE ONLY opencozy.device_web_session
    ADD CONSTRAINT device_web_session_exchange_hash_unique UNIQUE (exchange_hash);
ALTER TABLE ONLY opencozy.device_web_session
    ADD CONSTRAINT device_web_session_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.display_pairing
    ADD CONSTRAINT display_pairing_code_hash_unique UNIQUE (code_hash);
ALTER TABLE ONLY opencozy.display_pairing
    ADD CONSTRAINT display_pairing_household_id_id_unique UNIQUE (household_id, id);
ALTER TABLE ONLY opencozy.display_pairing
    ADD CONSTRAINT display_pairing_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.display_session
    ADD CONSTRAINT display_session_credential_hash_unique UNIQUE (credential_hash);
ALTER TABLE ONLY opencozy.display_session
    ADD CONSTRAINT display_session_pairing_id_unique UNIQUE (pairing_id);
ALTER TABLE ONLY opencozy.display_session
    ADD CONSTRAINT display_session_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_household_id_id_child_id_unique UNIQUE (household_id, id, child_id);
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_household_id_ledger_id_unique UNIQUE (household_id, ledger_id);
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_household_id_source_event_id_unique UNIQUE (household_id, source_event_id);
ALTER TABLE ONLY opencozy.external_identity_link
    ADD CONSTRAINT external_identity_link_pk PRIMARY KEY (household_id, child_id, provider, external_id, linked_at);
ALTER TABLE ONLY opencozy.gated_tv
    ADD CONSTRAINT gated_tv_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.gated_tv_mac
    ADD CONSTRAINT gated_tv_mac_household_id_mac_pk PRIMARY KEY (household_id, mac);
ALTER TABLE ONLY opencozy.home_assistant_connection
    ADD CONSTRAINT home_assistant_connection_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.home_assistant_entity
    ADD CONSTRAINT home_assistant_entity_household_id_connection_id_entity_id_pk PRIMARY KEY (household_id, connection_id, entity_id);
ALTER TABLE ONLY opencozy.household_owner
    ADD CONSTRAINT household_owner_pkey PRIMARY KEY (user_id);
ALTER TABLE ONLY opencozy.household
    ADD CONSTRAINT household_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.icloud_calendar
    ADD CONSTRAINT icloud_calendar_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.icloud_calendar
    ADD CONSTRAINT icloud_calendar_href_unique UNIQUE (household_id, calendar_href);
ALTER TABLE ONLY opencozy.icloud_connection
    ADD CONSTRAINT icloud_connection_pkey PRIMARY KEY (household_id);
ALTER TABLE ONLY opencozy.icloud_occurrence
    ADD CONSTRAINT icloud_occurrence_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.icloud_occurrence
    ADD CONSTRAINT icloud_occurrence_key_unique UNIQUE (household_id, series_id, recurrence_key);
ALTER TABLE ONLY opencozy.icloud_resource
    ADD CONSTRAINT icloud_resource_household_id_calendar_id_resource_href_pk PRIMARY KEY (household_id, calendar_id, resource_href);
ALTER TABLE ONLY opencozy.icloud_series
    ADD CONSTRAINT icloud_series_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.icloud_series
    ADD CONSTRAINT icloud_series_provider_uid_unique UNIQUE (household_id, calendar_id, provider_uid);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_command_unique UNIQUE (household_id, command_id);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_external_entry_unique UNIQUE (household_id, external_award_id, external_entry_type);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_reward_entry_unique UNIQUE (household_id, redemption_id, reward_entry_type);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_routine_entry_unique UNIQUE (household_id, routine_occurrence_id, routine_entry_type);
ALTER TABLE ONLY opencozy.meal_plan_entry
    ADD CONSTRAINT meal_plan_entry_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.meal_plan_entry
    ADD CONSTRAINT meal_plan_entry_slot_unique UNIQUE (household_id, local_day, meal_type);
ALTER TABLE ONLY opencozy.chore_occurrence
    ADD CONSTRAINT occurrence_chore_day_unique UNIQUE (household_id, chore_id, local_day);
ALTER TABLE ONLY opencozy.owner_bootstrap
    ADD CONSTRAINT owner_bootstrap_pkey PRIMARY KEY (singleton);
ALTER TABLE ONLY opencozy.parent_passkey
    ADD CONSTRAINT parent_passkey_credential_id_unique UNIQUE (credential_id);
ALTER TABLE ONLY opencozy.parent_passkey
    ADD CONSTRAINT parent_passkey_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.parent_session
    ADD CONSTRAINT parent_session_credential_hash_unique UNIQUE (credential_hash);
ALTER TABLE ONLY opencozy.parent_session
    ADD CONSTRAINT parent_session_pkey PRIMARY KEY (id);
ALTER TABLE ONLY opencozy.personal_credit
    ADD CONSTRAINT personal_credit_household_id_redemption_id_pk PRIMARY KEY (household_id, redemption_id);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT point_ledger_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.recipe
    ADD CONSTRAINT recipe_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.reward
    ADD CONSTRAINT reward_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_household_id_id_child_id_unique UNIQUE (household_id, id, child_id);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_screen_time_target_unique UNIQUE (household_id, id, child_id, screen_time_target_snapshot);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_screen_time_unique UNIQUE (household_id, id, child_id, screen_time_target_snapshot, screen_time_minutes_snapshot);
ALTER TABLE ONLY opencozy.routine_definition
    ADD CONSTRAINT routine_definition_household_id_id_assigned_child_id_unique UNIQUE (household_id, id, assigned_child_id);
ALTER TABLE ONLY opencozy.routine_definition
    ADD CONSTRAINT routine_definition_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.routine_occurrence
    ADD CONSTRAINT routine_occurrence_household_id_id_assigned_child_id_unique UNIQUE (household_id, id, assigned_child_id);
ALTER TABLE ONLY opencozy.routine_occurrence
    ADD CONSTRAINT routine_occurrence_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.routine_occurrence
    ADD CONSTRAINT routine_occurrence_household_id_routine_id_local_day_unique UNIQUE (household_id, routine_id, local_day);
ALTER TABLE ONLY opencozy.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);
ALTER TABLE ONLY opencozy.screen_time_event
    ADD CONSTRAINT screen_time_event_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.screen_time_grant_cut
    ADD CONSTRAINT screen_time_grant_cut_household_id_grant_id_pk PRIMARY KEY (household_id, grant_id);
ALTER TABLE ONLY opencozy.screen_time_grant
    ADD CONSTRAINT screen_time_grant_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.service_credential_award_rule
    ADD CONSTRAINT service_credential_award_rule_pk PRIMARY KEY (household_id, credential_id, rule_id);
ALTER TABLE ONLY opencozy.service_credential_child
    ADD CONSTRAINT service_credential_child_household_id_credential_id_child_id_pk PRIMARY KEY (household_id, credential_id, child_id);
ALTER TABLE ONLY opencozy.service_credential
    ADD CONSTRAINT service_credential_credential_hash_unique UNIQUE (credential_hash);
ALTER TABLE ONLY opencozy.service_credential
    ADD CONSTRAINT service_credential_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.time_request
    ADD CONSTRAINT time_request_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.tv_credit
    ADD CONSTRAINT tv_credit_household_id_redemption_id_pk PRIMARY KEY (household_id, redemption_id);
ALTER TABLE ONLY opencozy.tv_open_event
    ADD CONSTRAINT tv_open_event_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.tv_prompt
    ADD CONSTRAINT tv_prompt_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.tv_session
    ADD CONSTRAINT tv_session_household_id_id_pk PRIMARY KEY (household_id, id);
ALTER TABLE ONLY opencozy.webauthn_challenge
    ADD CONSTRAINT webauthn_challenge_pkey PRIMARY KEY (id);
CREATE INDEX auth_account_user_idx ON opencozy.auth_account USING btree (user_id);
CREATE INDEX auth_session_user_idx ON opencozy.auth_session USING btree (user_id);
CREATE INDEX auth_verification_identifier_idx ON opencozy.auth_verification USING btree (identifier);
CREATE UNIQUE INDEX award_rule_active_activity_unique ON opencozy.award_rule USING btree (household_id, activity_key) WHERE (archived_at IS NULL);
CREATE UNIQUE INDEX calendar_assignment_occurrence_unique ON opencozy.calendar_assignment USING btree (household_id, series_id, occurrence_key) WHERE (occurrence_key IS NOT NULL);
CREATE UNIQUE INDEX calendar_assignment_series_unique ON opencozy.calendar_assignment USING btree (household_id, series_id) WHERE (occurrence_key IS NULL);
CREATE INDEX calendar_occurrence_all_day_idx ON opencozy.calendar_occurrence USING btree (household_id, start_date) WHERE all_day;
CREATE INDEX calendar_occurrence_source_idx ON opencozy.calendar_occurrence USING btree (household_id, source_id);
CREATE INDEX calendar_occurrence_timed_idx ON opencozy.calendar_occurrence USING btree (household_id, starts_at) WHERE (NOT all_day);
CREATE UNIQUE INDEX calendar_source_feed_unique ON opencozy.calendar_source USING btree (household_id, feed_url) WHERE (feed_url IS NOT NULL);
CREATE UNIQUE INDEX calendar_source_google_unique ON opencozy.calendar_source USING btree (household_id, google_connection_id, google_calendar_id) WHERE (google_connection_id IS NOT NULL);
CREATE INDEX chore_occurrence_day_idx ON opencozy.chore_occurrence USING btree (household_id, local_day);
CREATE INDEX chore_occurrence_once_idx ON opencozy.chore_occurrence USING btree (household_id, local_day) WHERE (schedule_kind_snapshot = 'once'::text);
CREATE INDEX completion_approval_chore_idx ON opencozy.completion_approval USING btree (household_id, chore_occurrence_id) WHERE (chore_occurrence_id IS NOT NULL);
CREATE UNIQUE INDEX completion_approval_chore_pending ON opencozy.completion_approval USING btree (household_id, chore_occurrence_id) WHERE ((state = 'pending'::text) AND (chore_occurrence_id IS NOT NULL));
CREATE INDEX completion_approval_inbox_idx ON opencozy.completion_approval USING btree (household_id, requested_at, id) WHERE (state = 'pending'::text);
CREATE INDEX completion_approval_routine_idx ON opencozy.completion_approval USING btree (household_id, routine_occurrence_id) WHERE (routine_occurrence_id IS NOT NULL);
CREATE UNIQUE INDEX completion_approval_routine_pending ON opencozy.completion_approval USING btree (household_id, routine_occurrence_id) WHERE ((state = 'pending'::text) AND (routine_occurrence_id IS NOT NULL));
CREATE INDEX device_child_idx ON opencozy.device USING btree (household_id, child_id);
CREATE INDEX device_nonce_expiry_idx ON opencozy.device_nonce USING btree (device_id, expires_at);
CREATE UNIQUE INDEX device_push_token_unique ON opencozy.device USING btree (push_token) WHERE (push_token IS NOT NULL);
CREATE INDEX device_web_session_device_idx ON opencozy.device_web_session USING btree (household_id, device_id);
CREATE INDEX display_session_household_idx ON opencozy.display_session USING btree (household_id);
CREATE UNIQUE INDEX external_identity_link_active_unique ON opencozy.external_identity_link USING btree (household_id, provider, external_id) WHERE (unlinked_at IS NULL);
CREATE UNIQUE INDEX gated_tv_entity_active ON opencozy.gated_tv USING btree (household_id, ha_entity_id) WHERE ((archived_at IS NULL) AND (ha_entity_id IS NOT NULL));
CREATE INDEX gated_tv_mac_tv_idx ON opencozy.gated_tv_mac USING btree (household_id, tv_id);
CREATE UNIQUE INDEX home_assistant_connection_active_unique ON opencozy.home_assistant_connection USING btree (household_id) WHERE (revoked_at IS NULL);
CREATE INDEX household_owner_household_idx ON opencozy.household_owner USING btree (household_id);
CREATE INDEX icloud_occurrence_all_day_idx ON opencozy.icloud_occurrence USING btree (household_id, start_date) WHERE all_day;
CREATE INDEX icloud_occurrence_calendar_idx ON opencozy.icloud_occurrence USING btree (household_id, calendar_id);
CREATE INDEX icloud_occurrence_timed_idx ON opencozy.icloud_occurrence USING btree (household_id, starts_at) WHERE (NOT all_day);
CREATE INDEX meal_plan_entry_day_idx ON opencozy.meal_plan_entry USING btree (household_id, local_day);
CREATE INDEX parent_passkey_user_idx ON opencozy.parent_passkey USING btree (user_id);
CREATE INDEX parent_session_household_idx ON opencozy.parent_session USING btree (household_id);
CREATE UNIQUE INDEX parent_session_push_token_unique ON opencozy.parent_session USING btree (push_token) WHERE (push_token IS NOT NULL);
CREATE INDEX parent_session_web_session_idx ON opencozy.parent_session USING btree (web_session_id) WHERE (web_session_id IS NOT NULL);
CREATE INDEX personal_credit_child_idx ON opencozy.personal_credit USING btree (household_id, child_id, created_at);
CREATE INDEX point_ledger_child_idx ON opencozy.point_ledger USING btree (household_id, child_id, delta);
CREATE INDEX point_ledger_history_idx ON opencozy.point_ledger USING btree (household_id, created_at, id);
CREATE INDEX point_ledger_occurrence_idx ON opencozy.point_ledger USING btree (household_id, occurrence_id, delta) WHERE (occurrence_id IS NOT NULL);
CREATE INDEX routine_occurrence_day_idx ON opencozy.routine_occurrence USING btree (household_id, local_day);
CREATE INDEX screen_time_event_history_idx ON opencozy.screen_time_event USING btree (household_id, occurred_at, id);
CREATE INDEX screen_time_grant_cut_child_idx ON opencozy.screen_time_grant_cut USING btree (household_id, child_id, cut_at);
CREATE INDEX screen_time_grant_redemption_idx ON opencozy.screen_time_grant USING btree (household_id, redemption_id);
CREATE INDEX screen_time_grant_window_idx ON opencozy.screen_time_grant USING btree (household_id, child_id, target, ends_at);
CREATE INDEX time_request_child_idx ON opencozy.time_request USING btree (household_id, child_id, created_at);
CREATE UNIQUE INDEX time_request_pending_child ON opencozy.time_request USING btree (household_id, child_id) WHERE (state = 'pending'::text);
CREATE INDEX tv_credit_child_idx ON opencozy.tv_credit USING btree (household_id, child_id, created_at);
CREATE INDEX tv_open_event_at_idx ON opencozy.tv_open_event USING btree (household_id, at);
CREATE INDEX tv_prompt_inbox_idx ON opencozy.tv_prompt USING btree (household_id, created_at, id) WHERE (state = 'pending'::text);
CREATE UNIQUE INDEX tv_prompt_pending_tv ON opencozy.tv_prompt USING btree (household_id, tv_id) WHERE (state = 'pending'::text);
CREATE UNIQUE INDEX tv_session_open_child ON opencozy.tv_session USING btree (household_id, child_id) WHERE ((ended_at IS NULL) AND (watcher = 'child'::text));
CREATE UNIQUE INDEX tv_session_open_tv ON opencozy.tv_session USING btree (household_id, tv_id) WHERE (ended_at IS NULL);
CREATE INDEX tv_session_started_idx ON opencozy.tv_session USING btree (household_id, started_at);
CREATE INDEX webauthn_challenge_expiry_idx ON opencozy.webauthn_challenge USING btree (expires_at);
CREATE TRIGGER auth_session_revoke_parent_sessions AFTER DELETE ON opencozy.auth_session FOR EACH ROW EXECUTE FUNCTION opencozy.revoke_parent_sessions_for_web_session();
CREATE TRIGGER calendar_series_assignments_delete AFTER DELETE ON opencozy.calendar_series FOR EACH ROW EXECUTE FUNCTION opencozy.delete_calendar_series_assignments();
CREATE TRIGGER chore_reversal_no_truncate BEFORE TRUNCATE ON opencozy.chore_award_reversal FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER chore_reversal_no_update_delete BEFORE DELETE OR UPDATE ON opencozy.chore_award_reversal FOR EACH ROW EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER command_no_truncate BEFORE TRUNCATE ON opencozy.command FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER command_no_update_delete BEFORE DELETE OR UPDATE ON opencozy.command FOR EACH ROW EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER completion_approval_no_truncate BEFORE TRUNCATE ON opencozy.completion_approval FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER completion_approval_protect BEFORE DELETE OR UPDATE ON opencozy.completion_approval FOR EACH ROW EXECUTE FUNCTION opencozy.protect_completion_approval();
CREATE TRIGGER external_award_no_truncate BEFORE TRUNCATE ON opencozy.external_award FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER external_award_no_update_delete BEFORE DELETE OR UPDATE ON opencozy.external_award FOR EACH ROW EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER external_identity_link_no_truncate BEFORE TRUNCATE ON opencozy.external_identity_link FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER external_identity_link_protect BEFORE DELETE OR UPDATE ON opencozy.external_identity_link FOR EACH ROW EXECUTE FUNCTION opencozy.protect_external_identity_link();
CREATE TRIGGER icloud_series_assignments_delete AFTER DELETE ON opencozy.icloud_series FOR EACH ROW EXECUTE FUNCTION opencozy.delete_calendar_series_assignments();
CREATE TRIGGER point_ledger_no_truncate BEFORE TRUNCATE ON opencozy.point_ledger FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER point_ledger_no_update_delete BEFORE DELETE OR UPDATE ON opencozy.point_ledger FOR EACH ROW EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER reward_redemption_no_truncate BEFORE TRUNCATE ON opencozy.reward_redemption FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER reward_redemption_protect BEFORE DELETE OR UPDATE ON opencozy.reward_redemption FOR EACH ROW EXECUTE FUNCTION opencozy.protect_reward_redemption();
CREATE TRIGGER routine_occurrence_no_truncate BEFORE TRUNCATE ON opencozy.routine_occurrence FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER routine_occurrence_protect BEFORE INSERT OR DELETE OR UPDATE ON opencozy.routine_occurrence FOR EACH ROW EXECUTE FUNCTION opencozy.protect_routine_occurrence();
CREATE TRIGGER screen_time_grant_cut_guard BEFORE INSERT OR DELETE OR UPDATE ON opencozy.screen_time_grant_cut FOR EACH ROW EXECUTE FUNCTION opencozy.guard_screen_time_grant_cut();
CREATE TRIGGER screen_time_grant_cut_no_truncate BEFORE TRUNCATE ON opencozy.screen_time_grant_cut FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
CREATE TRIGGER screen_time_grant_guard BEFORE INSERT OR DELETE OR UPDATE ON opencozy.screen_time_grant FOR EACH ROW EXECUTE FUNCTION opencozy.guard_screen_time_grant();
CREATE TRIGGER screen_time_grant_no_truncate BEFORE TRUNCATE ON opencozy.screen_time_grant FOR EACH STATEMENT EXECUTE FUNCTION opencozy.reject_history_mutation();
ALTER TABLE ONLY opencozy.assistant_credential
    ADD CONSTRAINT assistant_credential_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.auth_account
    ADD CONSTRAINT auth_account_user_id_auth_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.auth_user(id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.auth_session
    ADD CONSTRAINT auth_session_user_id_auth_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.auth_user(id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.award_rule
    ADD CONSTRAINT award_rule_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.calendar_assignment
    ADD CONSTRAINT calendar_assignment_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.calendar_assignment_person
    ADD CONSTRAINT calendar_assignment_person_assignment_fk FOREIGN KEY (household_id, assignment_id) REFERENCES opencozy.calendar_assignment(household_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.calendar_assignment_person
    ADD CONSTRAINT calendar_assignment_person_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.calendar_google_connection
    ADD CONSTRAINT calendar_google_connection_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.calendar_occurrence
    ADD CONSTRAINT calendar_occurrence_series_fk FOREIGN KEY (household_id, source_id, series_id) REFERENCES opencozy.calendar_series(household_id, source_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.calendar_series
    ADD CONSTRAINT calendar_series_source_fk FOREIGN KEY (household_id, source_id) REFERENCES opencozy.calendar_source(household_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.calendar_source
    ADD CONSTRAINT calendar_source_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.child
    ADD CONSTRAINT child_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.chore_active_award
    ADD CONSTRAINT chore_active_award_ledger_fk FOREIGN KEY (household_id, ledger_id) REFERENCES opencozy.point_ledger(household_id, id);
ALTER TABLE ONLY opencozy.chore_active_award
    ADD CONSTRAINT chore_active_award_occurrence_fk FOREIGN KEY (household_id, occurrence_id) REFERENCES opencozy.chore_occurrence(household_id, id);
ALTER TABLE ONLY opencozy.chore_award_reversal
    ADD CONSTRAINT chore_award_reversal_award_fk FOREIGN KEY (household_id, award_id) REFERENCES opencozy.point_ledger(household_id, id);
ALTER TABLE ONLY opencozy.chore_award_reversal
    ADD CONSTRAINT chore_award_reversal_reversal_fk FOREIGN KEY (household_id, reversal_id) REFERENCES opencozy.point_ledger(household_id, id);
ALTER TABLE ONLY opencozy.chore_definition
    ADD CONSTRAINT chore_definition_child_fk FOREIGN KEY (household_id, assigned_child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.chore_definition
    ADD CONSTRAINT chore_definition_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.chore_occurrence
    ADD CONSTRAINT chore_occurrence_child_fk FOREIGN KEY (household_id, assigned_child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.chore_occurrence
    ADD CONSTRAINT chore_occurrence_definition_fk FOREIGN KEY (household_id, chore_id) REFERENCES opencozy.chore_definition(household_id, id);
ALTER TABLE ONLY opencozy.chore_occurrence
    ADD CONSTRAINT chore_occurrence_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.command
    ADD CONSTRAINT command_assistant_credential_fk FOREIGN KEY (household_id, assistant_credential_id) REFERENCES opencozy.assistant_credential(household_id, id);
ALTER TABLE ONLY opencozy.command
    ADD CONSTRAINT command_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_chore_fk FOREIGN KEY (household_id, chore_occurrence_id) REFERENCES opencozy.chore_occurrence(household_id, id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_device_fk FOREIGN KEY (household_id, device_id) REFERENCES opencozy.device(household_id, id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_ledger_fk FOREIGN KEY (household_id, ledger_id) REFERENCES opencozy.point_ledger(household_id, id);
ALTER TABLE ONLY opencozy.completion_approval
    ADD CONSTRAINT completion_approval_routine_fk FOREIGN KEY (household_id, routine_occurrence_id, child_id) REFERENCES opencozy.routine_occurrence(household_id, id, assigned_child_id);
ALTER TABLE ONLY opencozy.dashboard_view
    ADD CONSTRAINT dashboard_view_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.device
    ADD CONSTRAINT device_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.device
    ADD CONSTRAINT device_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.device_nonce
    ADD CONSTRAINT device_nonce_device_id_device_id_fk FOREIGN KEY (device_id) REFERENCES opencozy.device(id);
ALTER TABLE ONLY opencozy.device_pairing
    ADD CONSTRAINT device_pairing_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.device_pairing
    ADD CONSTRAINT device_pairing_device_fk FOREIGN KEY (household_id, device_id) REFERENCES opencozy.device(household_id, id);
ALTER TABLE ONLY opencozy.device_pairing
    ADD CONSTRAINT device_pairing_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.device_web_session
    ADD CONSTRAINT device_web_session_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.device_web_session
    ADD CONSTRAINT device_web_session_device_fk FOREIGN KEY (household_id, device_id) REFERENCES opencozy.device(household_id, id);
ALTER TABLE ONLY opencozy.device_web_session
    ADD CONSTRAINT device_web_session_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.display_pairing
    ADD CONSTRAINT display_pairing_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.display_session
    ADD CONSTRAINT display_session_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.display_session
    ADD CONSTRAINT display_session_pairing_fk FOREIGN KEY (household_id, pairing_id) REFERENCES opencozy.display_pairing(household_id, id);
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_ledger_fk FOREIGN KEY (household_id, ledger_id) REFERENCES opencozy.point_ledger(household_id, id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE ONLY opencozy.external_award
    ADD CONSTRAINT external_award_rule_fk FOREIGN KEY (household_id, rule_id) REFERENCES opencozy.award_rule(household_id, id);
ALTER TABLE ONLY opencozy.external_identity_link
    ADD CONSTRAINT external_identity_link_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.gated_tv
    ADD CONSTRAINT gated_tv_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.gated_tv_mac
    ADD CONSTRAINT gated_tv_mac_tv_fk FOREIGN KEY (household_id, tv_id) REFERENCES opencozy.gated_tv(household_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.home_assistant_connection
    ADD CONSTRAINT home_assistant_connection_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.home_assistant_entity
    ADD CONSTRAINT home_assistant_entity_connection_fk FOREIGN KEY (household_id, connection_id) REFERENCES opencozy.home_assistant_connection(household_id, id);
ALTER TABLE ONLY opencozy.household_owner
    ADD CONSTRAINT household_owner_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.household_owner
    ADD CONSTRAINT household_owner_user_id_auth_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.auth_user(id);
ALTER TABLE ONLY opencozy.icloud_calendar
    ADD CONSTRAINT icloud_calendar_household_id_icloud_connection_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.icloud_connection(household_id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.icloud_connection
    ADD CONSTRAINT icloud_connection_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.icloud_occurrence
    ADD CONSTRAINT icloud_occurrence_series_fk FOREIGN KEY (household_id, series_id) REFERENCES opencozy.icloud_series(household_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.icloud_resource
    ADD CONSTRAINT icloud_resource_calendar_fk FOREIGN KEY (household_id, calendar_id) REFERENCES opencozy.icloud_calendar(household_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.icloud_series
    ADD CONSTRAINT icloud_series_calendar_fk FOREIGN KEY (household_id, calendar_id) REFERENCES opencozy.icloud_calendar(household_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_external_fk FOREIGN KEY (household_id, external_award_id, child_id) REFERENCES opencozy.external_award(household_id, id, child_id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_occurrence_fk FOREIGN KEY (household_id, occurrence_id) REFERENCES opencozy.chore_occurrence(household_id, id);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_redemption_fk FOREIGN KEY (household_id, redemption_id, child_id) REFERENCES opencozy.reward_redemption(household_id, id, child_id);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT ledger_routine_fk FOREIGN KEY (household_id, routine_occurrence_id, child_id) REFERENCES opencozy.routine_occurrence(household_id, id, assigned_child_id);
ALTER TABLE ONLY opencozy.meal_plan_entry
    ADD CONSTRAINT meal_plan_entry_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.meal_plan_entry
    ADD CONSTRAINT meal_plan_entry_recipe_fk FOREIGN KEY (household_id, recipe_id) REFERENCES opencozy.recipe(household_id, id);
ALTER TABLE ONLY opencozy.owner_bootstrap
    ADD CONSTRAINT owner_bootstrap_user_id_household_owner_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.household_owner(user_id);
ALTER TABLE ONLY opencozy.parent_passkey
    ADD CONSTRAINT parent_passkey_user_id_auth_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.auth_user(id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.parent_session
    ADD CONSTRAINT parent_session_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.parent_session
    ADD CONSTRAINT parent_session_user_id_auth_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.auth_user(id) ON DELETE CASCADE;
ALTER TABLE ONLY opencozy.personal_credit
    ADD CONSTRAINT personal_credit_redemption_fk FOREIGN KEY (household_id, redemption_id, child_id, target, minutes) REFERENCES opencozy.reward_redemption(household_id, id, child_id, screen_time_target_snapshot, screen_time_minutes_snapshot);
ALTER TABLE ONLY opencozy.point_ledger
    ADD CONSTRAINT point_ledger_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.recipe
    ADD CONSTRAINT recipe_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.reward
    ADD CONSTRAINT reward_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_device_fk FOREIGN KEY (household_id, device_id) REFERENCES opencozy.device(household_id, id);
ALTER TABLE ONLY opencozy.reward_redemption
    ADD CONSTRAINT reward_redemption_reward_fk FOREIGN KEY (household_id, reward_id) REFERENCES opencozy.reward(household_id, id);
ALTER TABLE ONLY opencozy.routine_definition
    ADD CONSTRAINT routine_definition_child_fk FOREIGN KEY (household_id, assigned_child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.routine_definition
    ADD CONSTRAINT routine_definition_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.routine_occurrence
    ADD CONSTRAINT routine_occurrence_definition_fk FOREIGN KEY (household_id, routine_id, assigned_child_id) REFERENCES opencozy.routine_definition(household_id, id, assigned_child_id);
ALTER TABLE ONLY opencozy.screen_time_event
    ADD CONSTRAINT screen_time_event_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.screen_time_event
    ADD CONSTRAINT screen_time_event_device_fk FOREIGN KEY (household_id, device_id) REFERENCES opencozy.device(household_id, id);
ALTER TABLE ONLY opencozy.screen_time_grant_cut
    ADD CONSTRAINT screen_time_grant_cut_grant_fk FOREIGN KEY (household_id, grant_id) REFERENCES opencozy.screen_time_grant(household_id, id);
ALTER TABLE ONLY opencozy.screen_time_grant
    ADD CONSTRAINT screen_time_grant_redemption_fk FOREIGN KEY (household_id, redemption_id, child_id, target) REFERENCES opencozy.reward_redemption(household_id, id, child_id, screen_time_target_snapshot);
ALTER TABLE ONLY opencozy.service_credential_award_rule
    ADD CONSTRAINT service_credential_award_rule_credential_fk FOREIGN KEY (household_id, credential_id) REFERENCES opencozy.service_credential(household_id, id);
ALTER TABLE ONLY opencozy.service_credential_award_rule
    ADD CONSTRAINT service_credential_award_rule_rule_fk FOREIGN KEY (household_id, rule_id) REFERENCES opencozy.award_rule(household_id, id);
ALTER TABLE ONLY opencozy.service_credential_child
    ADD CONSTRAINT service_credential_child_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.service_credential_child
    ADD CONSTRAINT service_credential_child_credential_fk FOREIGN KEY (household_id, credential_id) REFERENCES opencozy.service_credential(household_id, id);
ALTER TABLE ONLY opencozy.service_credential
    ADD CONSTRAINT service_credential_household_id_household_id_fk FOREIGN KEY (household_id) REFERENCES opencozy.household(id);
ALTER TABLE ONLY opencozy.time_request
    ADD CONSTRAINT time_request_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.time_request
    ADD CONSTRAINT time_request_device_fk FOREIGN KEY (household_id, device_id) REFERENCES opencozy.device(household_id, id);
ALTER TABLE ONLY opencozy.time_request
    ADD CONSTRAINT time_request_redemption_fk FOREIGN KEY (household_id, redemption_id, child_id) REFERENCES opencozy.reward_redemption(household_id, id, child_id);
ALTER TABLE ONLY opencozy.tv_credit
    ADD CONSTRAINT tv_credit_redemption_fk FOREIGN KEY (household_id, redemption_id, child_id, target, minutes) REFERENCES opencozy.reward_redemption(household_id, id, child_id, screen_time_target_snapshot, screen_time_minutes_snapshot);
ALTER TABLE ONLY opencozy.tv_open_event
    ADD CONSTRAINT tv_open_event_assistant_fk FOREIGN KEY (household_id, assistant_credential_id) REFERENCES opencozy.assistant_credential(household_id, id);
ALTER TABLE ONLY opencozy.tv_open_event
    ADD CONSTRAINT tv_open_event_session_fk FOREIGN KEY (household_id, ended_session_id) REFERENCES opencozy.tv_session(household_id, id);
ALTER TABLE ONLY opencozy.tv_open_event
    ADD CONSTRAINT tv_open_event_tv_fk FOREIGN KEY (household_id, tv_id) REFERENCES opencozy.gated_tv(household_id, id);
ALTER TABLE ONLY opencozy.tv_prompt
    ADD CONSTRAINT tv_prompt_session_fk FOREIGN KEY (household_id, session_id) REFERENCES opencozy.tv_session(household_id, id);
ALTER TABLE ONLY opencozy.tv_prompt
    ADD CONSTRAINT tv_prompt_tv_fk FOREIGN KEY (household_id, tv_id) REFERENCES opencozy.gated_tv(household_id, id);
ALTER TABLE ONLY opencozy.tv_session
    ADD CONSTRAINT tv_session_child_fk FOREIGN KEY (household_id, child_id) REFERENCES opencozy.child(household_id, id);
ALTER TABLE ONLY opencozy.tv_session
    ADD CONSTRAINT tv_session_tv_fk FOREIGN KEY (household_id, tv_id) REFERENCES opencozy.gated_tv(household_id, id);
ALTER TABLE ONLY opencozy.webauthn_challenge
    ADD CONSTRAINT webauthn_challenge_user_id_auth_user_id_fk FOREIGN KEY (user_id) REFERENCES opencozy.auth_user(id) ON DELETE CASCADE;

INSERT INTO opencozy.schema_migrations VALUES ('0000_check_functions', '29f4517fd5bd34a9fd68865221c5ac7aff288ae614a63177ce6419993360f41c', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0001_tables', '13485d362755ad984f60c3f79d9cbe82aa983a8dcf3ffefdbf8ba03a199603f7', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0002_history_guards', 'b2d4ca6a4aaec62d5759a3d3521c090dc33a3961580db67e6d274fd558d95456', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0003_tutor_buddy', 'af80dd79849fa57bf2fe48f730edb14f8a6604d642dce6c1c543ea72a40cc24b', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0004_tutor_buddy_guards', '23bd27467fcc82246024e4c431e66e7bc7bb40e5709c5eab6325c2899bd1dd46', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0005_delivery_functions', '6a1a965e7481d96b7b6d7368abcfbbea47aa80ad677677e7ce0f268f9e754839', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0006_accepted_delivery', 'd52c7a282977f3885995a2ffad500cb945e09a66804d16522105a65ae4a9086e', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0007_calendar_assignment_cleanup', '81cee8503ad5cdc073c734a6b28c2c4bc7fc898aeb16c7913860c0c9fbe756f2', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0008_screen_time_rewards', '4eb53f9cdbc19ee10460bdcdeac19962aa4021dd6f5639b006fc2dd99d6e4fcc', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0009_screen_time_settlement', '1ccc278ae59db5b46773df4206151688002e235e61e3b64fe87de6bb218d5245', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0010_screen_time_fulfillment', '42809d420745e75ad28c57dffef9c8bd2ca922a4dc5b8d8d249f5373cbefa094', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0011_redemption_refunded_points', 'f090e4aaf8fbc42e6e0221572b18b066ef05c95a99a0b2d3a48a41aeb64f3f61', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0012_screen_time_cancel_guards', '8ecdc799365ead4d63d420a645748763c75638bc348f9201873d6527deb11352', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0013_redemption_refund_check', '7e5069eb4a58e6b332ef17101cdc06b136cae832a4f97276d99ff65604643ff2', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0014_approval_inbox_devices', '9d45adf24582c1f2010bcf735d7538d08534acf3efb0e460c92370bc4d82eccb', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0015_approval_guards_personal_target', '51241110acd9ce3e5a8e092394b815a8ffad02175ed4e7218203d0aa6c666090', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0016_screen_time_personal_only', '0fc0dfe7a240a563d6e791b14ffbdd0cb74eb0ba992e8c949e45571d066e7865', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0017_device_web_session', 'a0abd6791cb5fa45182a963a71360b7daf4787ac15e0f3a0ba822a460b980e48', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0018_parent_session_web_session', '368c63375ab5b6b1bfb6b47ee655d3b7c6733b233401a6f39cb7018101106dc3', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0019_parent_session_web_signout', '368c0fa106c59dcb29737997f04e9642abcda081de33d8f16c688d4ca7a51773', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0020_parent_screen_time_grant', 'a96af4de198e18b881523651697132db4725a3f76cc98746cfcbcf18eb31009f', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0021_assistant_credentials', '9f454af3a551fd50a53fd736220b5bad36e624c475a9b2a8fcb79eed2e45a09a', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0022_tv_gate', 'efbb58c2afd410e05a958d1132f7d735b5ead44ce0715dae6e68b355fae95397', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0023_tv_sessions', 'a4903047e3a9aba140f829dac1e3ab41f95a970e92fc5b0a99b7512e32e9da51', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0024_tv_backfill', '836c9414caf2174aab7e369ad9448e778fa91960c89bcf50e1439901f8119546', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0025_tv_macs', '00708e4bc8cf586662e916ef0b3b71b00b5f52668c0f5e9c7ac2114128839a50', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0026_tv_attempts', 'aee8a2dbb65e37006ba41b5dff14b2ea0856909296470862b39a308c8cfebfa6', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0027_tv_done', '0691d0edf180461a80607b2c134a94e76bcfd00b69bcafb78031549463f62f39', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0028_household_adults', '64854cd0693c4df407751b6b8ae9e8e5ac5168d41bac9cdc99e26b9e40c5837f', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0029_allowed_hours', '498988e306566c2252ca630c9e5976e1979a08ea47243ab0cab5ac039a6f9097', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0030_tv_watch_command', '88b616f690fa4451c981741f706139851af7fb10e587797feb9d325e879aa8a0', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0031_tv_open_hours', '5f5e77e2195a3dc1dd7ae137db2d7093cb90f50a73afc3699f6ff75151466212', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0032_banked_screen_time', 'b9bb92d19c030678e99d83cda7f942a36920b50ef6c654ff49b0e39a45d23780', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0033_banked_screen_time_guards', 'a212c833407dd3d49e3f52e44927f8e11da3b443dd74ab2378607b5aaaf1d5cc', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0034_tv_free_time_retired', '616406aba417b3c75bf99b8c010e365edf0d0cfcf9ed2927528d000c0c3c1c6c', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0035_tv_override', 'a7d7ae2c44e81c56f73ed6262f667af2ff93635cc2c768f7c58f53aba4bf38ba', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0036_household_lock_message', 'aa3d492837e70454aed52870fe38358548d02e4e34fb277c7eede5bc98c0309f', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0037_screen_time_events', '62dfcd622a2303843559f70481fc62e2585834035eb78272e5ce3b2da4640547', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0038_time_requests', 'd88974823ff2bc9722ad137855f45beb3219ab3d6a908bd64a1ad260be1eb46b', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0039_kid_pushes', '58afc5eed1da2f49f3ea8dc93f1df3393365c84546d4481a2158aa4cea4a212f', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0040_tv_open_days', 'c29617811b1b7b7a5dd459246641209d004b51d142400cb1a1906c58fe6ca11e', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0041_games_gate_scope', '342a7ab1f956421bf6e4c6ab6e6bafaba9aeedc92e5c039a787bbd2b75e2bd4c', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0042_household_web_filter', '4033b1db0c57bf8ab0299caaf19bc0de685aaf80b4f64cac099330d310d2f153', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0043_screen_time_grant_cuts', '25f0de5d3f46b69d27ff0175e15856679d936fa16fb4bb3367f86686b0038fd6', '2026-01-01 00:00:00+00');
INSERT INTO opencozy.schema_migrations VALUES ('0044_screen_time_grant_cut_guards', '92287594438b41bbf9ae2b84022684af5eea7911d4b0e7ff4c33f10b93245fe5', '2026-01-01 00:00:00+00');
