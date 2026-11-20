CREATE TABLE rules (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT    NOT NULL,
    enabled            INTEGER NOT NULL DEFAULT 1,
    protocol           TEXT    NOT NULL CHECK (protocol IN ('tcp','udp','both')),
    listen_ip          TEXT    NOT NULL DEFAULT '',
    listen_port_start  INTEGER NOT NULL,
    listen_port_end    INTEGER NOT NULL,
    target_host        TEXT    NOT NULL,
    target_port_start  INTEGER NOT NULL,
    udp_idle_timeout_s INTEGER NOT NULL DEFAULT 120,
    notes              TEXT    NOT NULL DEFAULT '',
    preset_group       TEXT    NOT NULL DEFAULT '',
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
);

CREATE TABLE bans (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cidr       TEXT    NOT NULL,
    rule_id    INTEGER REFERENCES rules(id) ON DELETE CASCADE,
    reason     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER
);

CREATE TABLE presets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    rules_json  TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE TABLE traffic_minute (
    rule_id      INTEGER NOT NULL,
    ts           INTEGER NOT NULL,
    bytes_in     INTEGER NOT NULL DEFAULT 0,
    bytes_out    INTEGER NOT NULL DEFAULT 0,
    conns_opened INTEGER NOT NULL DEFAULT 0,
    peak_conns   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (rule_id, ts)
) WITHOUT ROWID;

CREATE TABLE traffic_hour (
    rule_id      INTEGER NOT NULL,
    ts           INTEGER NOT NULL,
    bytes_in     INTEGER NOT NULL DEFAULT 0,
    bytes_out    INTEGER NOT NULL DEFAULT 0,
    conns_opened INTEGER NOT NULL DEFAULT 0,
    peak_conns   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (rule_id, ts)
) WITHOUT ROWID;

CREATE TABLE events (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    ts        INTEGER NOT NULL,
    level     TEXT    NOT NULL,
    type      TEXT    NOT NULL,
    rule_id   INTEGER,
    client_ip TEXT    NOT NULL DEFAULT '',
    message   TEXT    NOT NULL,
    meta_json TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX events_ts ON events(ts);
CREATE INDEX events_rule ON events(rule_id, ts);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE admin (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT    NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_ip    TEXT    NOT NULL DEFAULT '',
    user_agent TEXT    NOT NULL DEFAULT ''
);
