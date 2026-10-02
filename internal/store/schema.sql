-- One schema for fresh installations. No migrations, legacy aliases, or startup repairs.
PRAGMA application_id = 1380864852; -- RNST
PRAGMA user_version = 1;

CREATE TABLE schema_info (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    version INTEGER NOT NULL CHECK (version = 1),
    ddl_hash BLOB NOT NULL CHECK (length(ddl_hash) = 32),
    stream_key_hash BLOB NOT NULL CHECK (length(stream_key_hash) = 32),
    settings_key_hash BLOB NOT NULL CHECK (length(settings_key_hash) = 32)
) STRICT;
CREATE TABLE household (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    username TEXT NOT NULL CHECK (length(username) BETWEEN 1 AND 80),
    password_hash BLOB NOT NULL CHECK (length(password_hash) > 0)
) STRICT;
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY NOT NULL CHECK (length(token_hash) = 64),
    household_id INTEGER NOT NULL DEFAULT 1 REFERENCES household(singleton) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL CHECK (expires_at > 0)
) STRICT;
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE revisions (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    catalog INTEGER NOT NULL DEFAULT 1 CHECK (catalog > 0),
    favorites INTEGER NOT NULL DEFAULT 1 CHECK (favorites > 0),
    progress INTEGER NOT NULL DEFAULT 1 CHECK (progress > 0),
    metadata INTEGER NOT NULL DEFAULT 1 CHECK (metadata > 0)
) STRICT;
INSERT INTO revisions(singleton) VALUES (1);

CREATE TABLE libraries (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    root_path TEXT NOT NULL UNIQUE CHECK (substr(root_path, 1, 1) = '/'),
    root_identity TEXT NOT NULL CHECK (length(root_identity) > 0),
    kind TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    policy_revision INTEGER NOT NULL DEFAULT 1 CHECK (policy_revision > 0),
    created_at INTEGER NOT NULL CHECK (created_at > 0)
) STRICT;
CREATE TABLE library_sources (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('tmdb', 'tvdb', 'metatube')),
    priority INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 2),
    PRIMARY KEY (library_id, source),
    UNIQUE (library_id, priority)
) STRICT;
CREATE TABLE media_files (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL CHECK (length(relative_path) > 0 AND substr(relative_path,1,1) <> '/'
        AND relative_path NOT IN ('.', '..') AND relative_path NOT LIKE '../%'
        AND relative_path NOT LIKE '%/../%' AND relative_path NOT LIKE '%/..'),
    fingerprint TEXT NOT NULL CHECK (length(fingerprint) > 0),
    size INTEGER NOT NULL CHECK (size >= 0),
    modified_at INTEGER NOT NULL,
    added_at INTEGER NOT NULL CHECK (added_at > 0),
    state TEXT NOT NULL DEFAULT 'available' CHECK (state IN ('available', 'deleted')),
    organization_reason TEXT NOT NULL DEFAULT '',
    UNIQUE (library_id, relative_path, fingerprint),
    UNIQUE (id, library_id)
) STRICT;
CREATE INDEX media_files_recent ON media_files(added_at DESC, id);
CREATE TABLE series (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    title_key TEXT NOT NULL CHECK (length(title_key) > 0),
    local_title TEXT NOT NULL CHECK (length(local_title) > 0),
    UNIQUE (library_id, title_key),
    UNIQUE (id, library_id)
) STRICT;
CREATE TABLE file_organization (
    media_file_id TEXT PRIMARY KEY NOT NULL,
    library_id TEXT NOT NULL,
    series_id TEXT NOT NULL,
    season_number INTEGER NOT NULL CHECK (season_number >= 0),
    episode_number INTEGER NOT NULL CHECK (episode_number > 0),
    FOREIGN KEY (media_file_id, library_id) REFERENCES media_files(id, library_id) ON DELETE CASCADE,
    FOREIGN KEY (series_id, library_id) REFERENCES series(id, library_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX organization_episodes ON file_organization(series_id, season_number, episode_number, media_file_id);
CREATE TABLE watch_progress (
    media_file_id TEXT PRIMARY KEY NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    position REAL NOT NULL CHECK (position >= 0 AND position <= duration),
    duration REAL NOT NULL CHECK (duration > 0 AND duration <= 31622400),
    completed INTEGER NOT NULL CHECK (completed IN (0, 1) AND (completed = 0 OR position >= duration - 0.5)),
    updated_at INTEGER NOT NULL CHECK (updated_at > 0)
) STRICT;
CREATE INDEX progress_recent ON watch_progress(updated_at DESC, media_file_id) WHERE completed = 0 AND position > 0;
CREATE TABLE media_favorites (
    media_file_id TEXT PRIMARY KEY NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    favorited_at INTEGER NOT NULL CHECK (favorited_at > 0)
) STRICT;
CREATE INDEX media_favorites_recent ON media_favorites(favorited_at DESC, media_file_id);
CREATE TABLE series_favorites (
    series_id TEXT PRIMARY KEY NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    favorited_at INTEGER NOT NULL CHECK (favorited_at > 0)
) STRICT;
CREATE INDEX series_favorites_recent ON series_favorites(favorited_at DESC, series_id);
CREATE TABLE scan_runs (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'completed', 'failed', 'interrupted')),
    requested_at INTEGER NOT NULL CHECK (requested_at > 0),
    started_at INTEGER,
    finished_at INTEGER,
    discovered_count INTEGER NOT NULL DEFAULT 0 CHECK (discovered_count >= 0),
    error_code TEXT,
    UNIQUE (id, library_id),
    CHECK (started_at IS NULL OR started_at >= requested_at),
    CHECK (state NOT IN ('running','completed') OR started_at IS NOT NULL),
    CHECK (state <> 'queued' OR started_at IS NULL),
    CHECK (finished_at IS NULL OR finished_at >= coalesce(started_at, requested_at)),
    CHECK ((state IN ('queued','running') AND finished_at IS NULL) OR (state IN ('completed','failed','interrupted') AND finished_at IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX scan_one_active ON scan_runs(library_id) WHERE state IN ('queued', 'running');
CREATE INDEX scans_library ON scan_runs(library_id, requested_at DESC, id);
CREATE TABLE scan_entries (
    scan_id TEXT NOT NULL,
    library_id TEXT NOT NULL,
    relative_path TEXT NOT NULL CHECK (length(relative_path) > 0 AND substr(relative_path,1,1) <> '/'
        AND relative_path NOT IN ('.', '..') AND relative_path NOT LIKE '../%'
        AND relative_path NOT LIKE '%/../%' AND relative_path NOT LIKE '%/..'),
    fingerprint TEXT NOT NULL CHECK (length(fingerprint) > 0),
    size INTEGER NOT NULL CHECK (size >= 0),
    modified_at INTEGER NOT NULL,
    PRIMARY KEY (scan_id, relative_path),
    FOREIGN KEY (scan_id, library_id) REFERENCES scan_runs(id, library_id) ON DELETE CASCADE
) STRICT;

-- Pure filename organization staged with the discovered file, never published
-- independently of the complete scan. No full in-memory file manifest.
CREATE TABLE scan_organization (
    scan_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    title_key TEXT NOT NULL,
    local_title TEXT NOT NULL,
    season_number INTEGER NOT NULL,
    episode_number INTEGER NOT NULL,
    reason TEXT NOT NULL,
    PRIMARY KEY (scan_id, relative_path),
    FOREIGN KEY (scan_id, relative_path) REFERENCES scan_entries(scan_id, relative_path) ON DELETE CASCADE
) STRICT;

CREATE TABLE provider_catalogs (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    source TEXT NOT NULL CHECK (source IN ('tmdb', 'tvdb', 'metatube')),
    base_url TEXT NOT NULL,
    UNIQUE (source, base_url),
    UNIQUE (id, source),
    CHECK ((source = 'tmdb' AND id = 'tmdb' AND base_url = 'https://api.themoviedb.org/3')
        OR (source = 'tvdb' AND id = 'tvdb' AND base_url = 'https://api4.thetvdb.com/v4')
        OR (source = 'metatube' AND (base_url LIKE 'https://%' OR base_url LIKE 'http://%')))
) STRICT;
INSERT INTO provider_catalogs VALUES ('tmdb', 'tmdb', 'https://api.themoviedb.org/3'), ('tvdb', 'tvdb', 'https://api4.thetvdb.com/v4');
CREATE TABLE provider_settings (
    source TEXT PRIMARY KEY NOT NULL CHECK (source IN ('tmdb', 'tvdb', 'metatube')),
    catalog_id TEXT,
    secrets BLOB, -- AES-256-GCM; NULL means never configured. No plaintext or legacy import.
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    FOREIGN KEY (catalog_id, source) REFERENCES provider_catalogs(id, source),
    CHECK (source = 'metatube' OR (catalog_id IS NOT NULL AND catalog_id = source)),
    CHECK (secrets IS NULL OR length(secrets) >= 28)
) STRICT;
INSERT INTO provider_settings(source, catalog_id) VALUES ('tmdb', 'tmdb'), ('tvdb', 'tvdb'), ('metatube', NULL);
CREATE TABLE remote_titles (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    catalog_id TEXT NOT NULL REFERENCES provider_catalogs(id),
    namespace TEXT NOT NULL CHECK (length(namespace) > 0),
    kind TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    remote_id TEXT NOT NULL CHECK (length(remote_id) > 0),
    UNIQUE (catalog_id, namespace, kind, remote_id),
    UNIQUE (id, kind),
    UNIQUE (id, catalog_id)
) STRICT;
CREATE TRIGGER remote_title_namespace BEFORE INSERT ON remote_titles BEGIN
    SELECT CASE WHEN EXISTS (SELECT 1 FROM provider_catalogs p WHERE p.id = NEW.catalog_id
        AND ((p.source IN ('tmdb','tvdb') AND NEW.namespace <> p.source) OR (p.source = 'metatube' AND NEW.kind <> 'movie')))
        THEN RAISE(ABORT, 'invalid provider namespace or kind') END;
END;
-- External identities are immutable. A changed address/identity means a new row.
CREATE TRIGGER remote_title_immutable BEFORE UPDATE ON remote_titles BEGIN
    SELECT RAISE(ABORT, 'remote title identity is immutable');
END;
CREATE TRIGGER provider_catalog_immutable BEFORE UPDATE ON provider_catalogs BEGIN
    SELECT RAISE(ABORT, 'provider catalog identity is immutable');
END;
CREATE TABLE metadata_subjects (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    library_id TEXT NOT NULL,
    expected_kind TEXT NOT NULL CHECK (expected_kind IN ('movie', 'series')),
    media_file_id TEXT UNIQUE,
    series_id TEXT UNIQUE,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    CHECK ((media_file_id IS NOT NULL) <> (series_id IS NOT NULL)),
    CHECK (series_id IS NULL OR expected_kind = 'series'),
    FOREIGN KEY (media_file_id, library_id) REFERENCES media_files(id, library_id) ON DELETE CASCADE,
    FOREIGN KEY (series_id, library_id) REFERENCES series(id, library_id) ON DELETE CASCADE,
    UNIQUE (id, expected_kind),
    UNIQUE (id, library_id)
) STRICT;
CREATE INDEX subjects_library ON metadata_subjects(library_id, id);
CREATE TRIGGER subject_valid_insert BEFORE INSERT ON metadata_subjects BEGIN
    SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM libraries WHERE id = NEW.library_id AND kind = NEW.expected_kind)
        OR EXISTS (SELECT 1 FROM file_organization WHERE media_file_id = NEW.media_file_id)
        THEN RAISE(ABORT, 'invalid metadata subject') END;
END;
CREATE TRIGGER subject_identity_immutable BEFORE UPDATE OF id, library_id, expected_kind, media_file_id, series_id ON metadata_subjects BEGIN
    SELECT RAISE(ABORT, 'replace changed metadata subjects transactionally');
END;
-- The scan publisher must remove the old file subject before organizing it.
CREATE TRIGGER organization_no_file_subject BEFORE INSERT ON file_organization BEGIN
    SELECT CASE WHEN EXISTS (SELECT 1 FROM metadata_subjects WHERE media_file_id = NEW.media_file_id)
        THEN RAISE(ABORT, 'remove old file subject before organization') END;
    SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM libraries WHERE id = NEW.library_id AND kind = 'series')
        THEN RAISE(ABORT, 'only series libraries can organize episodes') END;
END;
CREATE TRIGGER organization_replace BEFORE UPDATE ON file_organization BEGIN
    SELECT RAISE(ABORT, 'replace organization transactionally');
END;
CREATE TABLE metadata_bindings (
    subject_id TEXT PRIMARY KEY NOT NULL,
    expected_kind TEXT NOT NULL,
    remote_title_id TEXT NOT NULL,
    selection TEXT NOT NULL CHECK (selection IN ('automatic', 'manual')),
    revision INTEGER NOT NULL CHECK (revision > 0),
    attempted_at INTEGER,
    attempt_state TEXT NOT NULL DEFAULT 'never' CHECK (attempt_state IN ('never','succeeded','failed')),
    error_code TEXT,
    FOREIGN KEY (subject_id, expected_kind) REFERENCES metadata_subjects(id, expected_kind) ON DELETE CASCADE,
    FOREIGN KEY (remote_title_id, expected_kind) REFERENCES remote_titles(id, kind)
) STRICT;
CREATE INDEX bindings_title ON metadata_bindings(remote_title_id, subject_id);
CREATE TABLE metadata_snapshots (
    remote_title_id TEXT PRIMARY KEY NOT NULL REFERENCES remote_titles(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) > 0),
    original_title TEXT NOT NULL,
    identifier TEXT NOT NULL DEFAULT '',
    year INTEGER CHECK (year BETWEEN 1 AND 9999),
    overview TEXT NOT NULL,
    source_url TEXT NOT NULL,
    fetched_at INTEGER NOT NULL CHECK (fetched_at > 0),
    expires_at INTEGER NOT NULL CHECK (expires_at > fetched_at),
    revision INTEGER NOT NULL CHECK (revision > 0),
    credits_state TEXT NOT NULL CHECK (credits_state IN ('missing','ready','failed')),
    artwork_state TEXT NOT NULL CHECK (artwork_state IN ('missing','ready','failed')),
    credits_retry_at INTEGER,
    artwork_retry_at INTEGER
) STRICT;
CREATE INDEX snapshots_expiry ON metadata_snapshots(expires_at, remote_title_id);
-- Stable scoped people; names/roles/photos live only in current title_credits.
CREATE TABLE people (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    catalog_id TEXT NOT NULL REFERENCES provider_catalogs(id),
    person_key TEXT NOT NULL CHECK (length(person_key) > 0),
    UNIQUE (catalog_id, person_key),
    UNIQUE (id, catalog_id)
) STRICT;
CREATE TABLE title_credits (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    remote_title_id TEXT NOT NULL REFERENCES metadata_snapshots(remote_title_id) ON DELETE CASCADE,
    catalog_id TEXT NOT NULL,
    person_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('actor', 'director')),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    name TEXT NOT NULL CHECK (length(name) > 0),
    characters TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(characters) AND json_type(characters) = 'array'),
    FOREIGN KEY (remote_title_id, catalog_id) REFERENCES remote_titles(id, catalog_id),
    FOREIGN KEY (person_id, catalog_id) REFERENCES people(id, catalog_id),
    UNIQUE (remote_title_id, role, ordinal),
    UNIQUE (id, remote_title_id)
) STRICT;
CREATE INDEX credits_person_works ON title_credits(catalog_id, person_id, remote_title_id);
CREATE TABLE assets (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) > 0),
    remote_title_id TEXT NOT NULL REFERENCES metadata_snapshots(remote_title_id) ON DELETE CASCADE,
    catalog_id TEXT NOT NULL,
    credit_id TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('primary','thumb','backdrop','photo')),
    reference TEXT NOT NULL CHECK (length(reference) > 0), -- adapter-controlled ref, never a client URL
    revision INTEGER NOT NULL CHECK (revision > 0),
    provider_revision INTEGER NOT NULL CHECK (provider_revision > 0),
    state TEXT NOT NULL DEFAULT 'missing' CHECK (state IN ('missing','ready','cooling')),
    bytes BLOB,
    mime TEXT CHECK (mime IN ('image/jpeg','image/png','image/webp','image/gif')),
    expires_at INTEGER CHECK (expires_at > 0),
    retry_at INTEGER CHECK (retry_at > 0),
    CHECK ((kind = 'photo') = (credit_id IS NOT NULL)),
    CHECK (bytes IS NULL OR length(bytes) <= 5242880),
    CHECK ((state = 'ready' AND bytes IS NOT NULL AND length(bytes) > 0 AND mime IS NOT NULL AND expires_at IS NOT NULL)
        OR (state <> 'ready' AND bytes IS NULL AND mime IS NULL AND expires_at IS NULL)),
    CHECK (state <> 'cooling' OR retry_at IS NOT NULL),
    FOREIGN KEY (remote_title_id, catalog_id) REFERENCES remote_titles(id, catalog_id),
    FOREIGN KEY (credit_id, remote_title_id) REFERENCES title_credits(id, remote_title_id) ON DELETE CASCADE
) STRICT;
CREATE UNIQUE INDEX assets_title_kind ON assets(remote_title_id, kind) WHERE credit_id IS NULL;
CREATE UNIQUE INDEX assets_credit_photo ON assets(credit_id) WHERE credit_id IS NOT NULL;
CREATE INDEX assets_expiry ON assets(expires_at);
CREATE TABLE metadata_jobs (
    subject_id TEXT PRIMARY KEY NOT NULL REFERENCES metadata_subjects(id) ON DELETE CASCADE,
    requested_generation INTEGER NOT NULL CHECK (requested_generation > 0),
    claimed_generation INTEGER CHECK (claimed_generation > 0 AND claimed_generation <= requested_generation),
    state TEXT NOT NULL CHECK (state IN ('queued','running','succeeded','failed','superseded')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    subject_revision INTEGER,
    binding_revision INTEGER CHECK (binding_revision >= 0),
    policy_revision INTEGER,
    catalog_id TEXT REFERENCES provider_catalogs(id),
    requested_at INTEGER NOT NULL CHECK (requested_at > 0),
    started_at INTEGER,
    finished_at INTEGER,
    next_attempt_at INTEGER,
    error_code TEXT,
    CHECK (state <> 'running' OR (claimed_generation IS NOT NULL AND subject_revision IS NOT NULL AND subject_revision > 0 AND binding_revision IS NOT NULL AND policy_revision IS NOT NULL AND policy_revision > 0 AND started_at IS NOT NULL))
) STRICT;
CREATE INDEX jobs_pending ON metadata_jobs(state, next_attempt_at, subject_id);
CREATE TABLE metadata_job_providers (
    subject_id TEXT NOT NULL REFERENCES metadata_jobs(subject_id) ON DELETE CASCADE,
    source TEXT NOT NULL REFERENCES provider_settings(source),
    revision INTEGER NOT NULL CHECK (revision > 0),
    catalog_id TEXT,
    PRIMARY KEY (subject_id, source),
    FOREIGN KEY (catalog_id, source) REFERENCES provider_catalogs(id, source)
) STRICT;

-- Rebuildable read projection; no progress/favorite copies, payloads, or image BLOBs.
CREATE TABLE catalog_entries (
    entry_key TEXT PRIMARY KEY NOT NULL CHECK (length(entry_key) > 0),
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    media_file_id TEXT UNIQUE,
    series_id TEXT UNIQUE,
    subject_id TEXT REFERENCES metadata_subjects(id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK (kind IN ('movie','series')),
    local_title TEXT NOT NULL,
    local_search_key TEXT NOT NULL,
    title_sort_key TEXT NOT NULL,
    added_at INTEGER NOT NULL CHECK (added_at > 0),
    season_count INTEGER NOT NULL DEFAULT 0 CHECK (season_count >= 0),
    episode_count INTEGER NOT NULL DEFAULT 0 CHECK (episode_count >= 0),
    file_count INTEGER NOT NULL CHECK (file_count > 0),
    available_file_count INTEGER NOT NULL CHECK (available_file_count BETWEEN 0 AND file_count),
    CHECK ((media_file_id IS NOT NULL) <> (series_id IS NOT NULL)),
    CHECK (series_id IS NULL OR kind = 'series'),
    FOREIGN KEY (media_file_id, library_id) REFERENCES media_files(id, library_id) ON DELETE CASCADE,
    FOREIGN KEY (series_id, library_id) REFERENCES series(id, library_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX entries_library_title ON catalog_entries(library_id, title_sort_key, entry_key);
CREATE INDEX entries_title ON catalog_entries(title_sort_key, entry_key);
CREATE INDEX entries_recent ON catalog_entries(added_at DESC, entry_key);
CREATE INDEX entries_library_recent ON catalog_entries(library_id, added_at DESC, entry_key);
CREATE INDEX entries_subject ON catalog_entries(subject_id);

CREATE TRIGGER entries_subject_library_insert BEFORE INSERT ON catalog_entries WHEN NEW.subject_id IS NOT NULL BEGIN
    SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM metadata_subjects WHERE id=NEW.subject_id AND library_id=NEW.library_id)
        THEN RAISE(ABORT, 'projection subject must belong to the same library') END;
END;
CREATE TRIGGER entries_subject_library_update BEFORE UPDATE OF subject_id,library_id ON catalog_entries WHEN NEW.subject_id IS NOT NULL BEGIN
    SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM metadata_subjects WHERE id=NEW.subject_id AND library_id=NEW.library_id)
        THEN RAISE(ABORT, 'projection subject must belong to the same library') END;
END;
