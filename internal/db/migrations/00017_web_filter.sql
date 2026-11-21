-- +goose Up
-- Blocked websites (slice 8b): the household's two lists, enforced on each child's iPad in every
-- browser and by the home router. Game portals are always blocked; games only while the child has
-- no iPad time running. Lowercase names like example.com, at most 500 per list; a name covers its
-- subdomains. A name on both lists is refused by the API (a portal is always blocked anyway).
ALTER TABLE household
    ADD COLUMN game_portal_domains text[] NOT NULL DEFAULT '{}',
    ADD COLUMN game_domains        text[] NOT NULL DEFAULT '{}',
    ADD CONSTRAINT household_game_portal_domains_valid CHECK (
        array_position(game_portal_domains, NULL) IS NULL AND cardinality(game_portal_domains) <= 500
        AND array_to_string(game_portal_domains, ' ') ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+( [a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+)*)?$'),
    ADD CONSTRAINT household_game_domains_valid CHECK (
        array_position(game_domains, NULL) IS NULL AND cardinality(game_domains) <= 500
        AND array_to_string(game_domains, ' ') ~ '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+( [a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+)*)?$');

-- +goose Down
ALTER TABLE household
    DROP CONSTRAINT household_game_domains_valid,
    DROP CONSTRAINT household_game_portal_domains_valid,
    DROP COLUMN game_domains,
    DROP COLUMN game_portal_domains;
