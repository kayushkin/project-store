-- Hands out each id number once, so an id written on a card never comes to
-- mean a different project.
CREATE TABLE IF NOT EXISTS id_sequences (
    name TEXT PRIMARY KEY,
    next INTEGER NOT NULL
);

-- A project: what it is for. What happens in it is not stored here; it is
-- worked out from what the project owns (project_links) and from the cards
-- kanban-store links to it.
CREATE TABLE IF NOT EXISTS projects (
    id                 TEXT PRIMARY KEY,          -- project_000001
    name               TEXT NOT NULL,
    kind               TEXT NOT NULL,             -- project | stream
    parent_id          TEXT REFERENCES projects (id),
    goal               TEXT NOT NULL DEFAULT '',
    done_when          TEXT NOT NULL DEFAULT '',
    stage              TEXT NOT NULL,
    -- The share of all spend the user means this project to take, 0 to 1;
    -- NULL when nobody set one.
    spend_target_share REAL,
    created_by         TEXT NOT NULL DEFAULT '',
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    archived_at        INTEGER NOT NULL DEFAULT 0
);

-- What a project owns, each thing named by the id its own store gave it.
-- label is the owner's name for it when linked, for display only.
CREATE TABLE IF NOT EXISTS project_links (
    id          INTEGER PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id),
    entity_type TEXT NOT NULL,
    entity_ref  TEXT NOT NULL,
    label       TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    UNIQUE (project_id, entity_type, entity_ref)
);
CREATE INDEX IF NOT EXISTS project_links_by_entity ON project_links (entity_type, entity_ref);

-- Every change to a project's own fields, oldest first: how its stage moved
-- and who moved it.
CREATE TABLE IF NOT EXISTS project_changes (
    id         INTEGER PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects (id),
    field      TEXT NOT NULL,
    old_value  TEXT NOT NULL,
    new_value  TEXT NOT NULL,
    changed_by TEXT NOT NULL DEFAULT '',
    changed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS project_changes_by_project ON project_changes (project_id, id);
