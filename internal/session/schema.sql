CREATE TABLE IF NOT EXISTS sessions (
    id                TEXT PRIMARY KEY,
    title             TEXT NOT NULL DEFAULT 'New Session',
    parent_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    content      TEXT,
    thinking      TEXT,
    tool_calls   TEXT,
    tool_call_id TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tool_results (
    id         TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_name  TEXT NOT NULL,
    params     TEXT NOT NULL,
    result     TEXT,
    approved   BOOLEAN NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tool_results_message ON tool_results(message_id);
