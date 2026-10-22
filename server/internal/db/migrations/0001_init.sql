-- 0001_init: initial Luma schema.
-- rollback: there is no automatic down migration. Restore the backup taken before the
-- upgrade and deploy the previous image tag (deploy/RUNBOOK.md, "Rollback").

CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username       citext NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9_]{3,24}$'),
    display_name   text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 64),
    avatar_file_id uuid,
    password_hash  text NOT NULL,
    -- Per-user counter for user_events.seq; bumped under row lock inside the writing transaction.
    event_seq      bigint NOT NULL DEFAULT 0,
    is_admin       boolean NOT NULL DEFAULT false,
    disabled_at    timestamptz,
    last_seen_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE,
    device_name  text NOT NULL DEFAULT '' CHECK (char_length(device_name) <= 64),
    platform     text NOT NULL DEFAULT '' CHECK (char_length(platform) <= 16),
    ip           inet,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at   timestamptz
);
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE invites (
    code_hash  bytea PRIMARY KEY,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    used_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    used_at    timestamptz
);
CREATE INDEX invites_creator_open_idx ON invites (created_by) WHERE used_at IS NULL;

CREATE TABLE blocks (
    blocker_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);
CREATE INDEX blocks_blocked_idx ON blocks (blocked_id);

CREATE TABLE files (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    conversation_id uuid,
    name            text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
    mime            text NOT NULL,
    size            bigint NOT NULL CHECK (size >= 0),
    width           int,
    height          int,
    sha256          bytea NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE conversations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind             text NOT NULL CHECK (kind IN ('direct', 'group')),
    title            text CHECK (title IS NULL OR char_length(title) BETWEEN 1 AND 80),
    avatar_file_id   uuid REFERENCES files (id) ON DELETE SET NULL,
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- Highest allocated message seq. Bumped under row lock in the send transaction, so seq is gap-free.
    last_seq         bigint NOT NULL DEFAULT 0,
    last_activity_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE files ADD FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE;

-- One direct conversation per unordered pair of users (user_a < user_b).
CREATE TABLE direct_pairs (
    user_a          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_b          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL UNIQUE REFERENCES conversations (id) ON DELETE CASCADE,
    PRIMARY KEY (user_a, user_b),
    CHECK (user_a < user_b)
);

-- A member sees messages with joined_seq < seq <= coalesce(left_seq, +inf).
CREATE TABLE members (
    conversation_id    uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role               text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member')),
    joined_at          timestamptz NOT NULL DEFAULT now(),
    joined_seq         bigint NOT NULL DEFAULT 0,
    left_at            timestamptz,
    left_seq           bigint,
    last_read_seq      bigint NOT NULL DEFAULT 0,
    last_delivered_seq bigint NOT NULL DEFAULT 0,
    muted_until        timestamptz,
    archived           boolean NOT NULL DEFAULT false,
    PRIMARY KEY (conversation_id, user_id)
);
CREATE INDEX members_user_active_idx ON members (user_id) WHERE left_at IS NULL;

CREATE TABLE messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    seq             bigint NOT NULL,
    sender_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Client-generated idempotency key; a retry with the same (sender, client_id) returns the original.
    client_id       uuid NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('text', 'file', 'system')),
    body            text NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
    -- Envelope extension point: attachments now; reply_to, reactions, voice later.
    meta            jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    edited_at       timestamptz,
    deleted_at      timestamptz,
    tsv             tsvector GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED,
    UNIQUE (conversation_id, seq),
    UNIQUE (sender_id, client_id)
);
CREATE INDEX messages_tsv_idx ON messages USING gin (tsv);

-- Durable per-user event log used for gap-free reconnect replay.
CREATE TABLE user_events (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seq        bigint NOT NULL,
    type       text NOT NULL,
    payload    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, seq)
);
CREATE INDEX user_events_created_idx ON user_events (created_at);
