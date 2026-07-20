-- Structure entities: bounded, kept forever. Common shape per spec.md 3:
-- discord_id, stoat_id (NULL until active), status, canonical_state JSON, timestamps.

CREATE TABLE server_map (
    discord_id TEXT PRIMARY KEY,
    stoat_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting')),
    canonical_state TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE category_map (
    discord_id TEXT PRIMARY KEY,
    stoat_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting')),
    canonical_state TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE channel_map (
    discord_id TEXT PRIMARY KEY,
    stoat_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting')),
    canonical_state TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE role_map (
    discord_id TEXT PRIMARY KEY,
    stoat_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting')),
    canonical_state TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE emoji_map (
    discord_id TEXT PRIMARY KEY,
    stoat_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting')),
    canonical_state TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- Unbounded, pruned per spec.md 8 retention (30 days). Its own shape, not the common one.
CREATE TABLE message_map (
    discord_msg_id TEXT PRIMARY KEY,
    stoat_msg_id TEXT,
    channel_id TEXT NOT NULL REFERENCES channel_map (discord_id),
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX idx_message_map_channel_id ON message_map (channel_id);

-- Never pruned; survives message_map pruning to keep backfill working (spec.md 8).
CREATE TABLE channel_cursor (
    channel_id TEXT PRIMARY KEY REFERENCES channel_map (discord_id),
    last_synced_discord_msg_id TEXT NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- Durable queue for degraded-window events (spec.md 8).
CREATE TABLE op_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    op_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting')),
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);
