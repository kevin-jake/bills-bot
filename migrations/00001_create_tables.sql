-- +goose Up

-- A free-form heading the Board groups Bills under, e.g. "BDO", "Utilities", "Investment".
CREATE TABLE sections (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    display_order INTEGER  NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- The standing list: a thing the household pays every month.
CREATE TABLE bills (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT     NOT NULL,
    aliases       TEXT     NOT NULL DEFAULT '',
    section_id    INTEGER  NOT NULL REFERENCES sections(id),
    channel       TEXT     NOT NULL CHECK (channel IN
                      ('kevin_direct','sheena_bdo','sheena_bpi','sheena_psbank','charged_to_card')),
    card_name     TEXT,
    display_order INTEGER  NOT NULL,
    archived_at   DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((channel = 'charged_to_card') = (card_name IS NOT NULL))
);
CREATE UNIQUE INDEX bills_active_name_uq ON bills(name COLLATE NOCASE) WHERE archived_at IS NULL;
CREATE INDEX bills_section_idx ON bills(section_id, display_order);

-- One month's copy of the Bill list.
CREATE TABLE cycles (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    month            TEXT     NOT NULL UNIQUE CHECK (length(month) = 7),
    opened_at        DATETIME NOT NULL,
    closed_at        DATETIME,
    board_chat_id    INTEGER,
    board_message_id INTEGER
);
CREATE INDEX cycles_open_idx ON cycles(closed_at, month);

-- One Bill's obligation within one Cycle. amount_cents NULL means the amount is not known yet.
CREATE TABLE payables (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    cycle_id     INTEGER  NOT NULL REFERENCES cycles(id),
    bill_id      INTEGER  NOT NULL REFERENCES bills(id),
    amount_cents INTEGER  CHECK (amount_cents IS NULL OR amount_cents >= 0),
    status       TEXT     NOT NULL DEFAULT 'due' CHECK (status IN ('due','funded','paid')),
    channel      TEXT     NOT NULL,
    card_name    TEXT,
    paid_at      DATETIME,
    paid_by      INTEGER,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (cycle_id, bill_id)
);
CREATE INDEX payables_bill_idx ON payables(bill_id, cycle_id);
CREATE INDEX payables_cycle_idx ON payables(cycle_id, status);

-- Money Kevin moves into one of Sheena's accounts so she can pay that channel's Bills.
CREATE TABLE transfers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cycle_id   INTEGER  NOT NULL REFERENCES cycles(id),
    channel    TEXT     NOT NULL CHECK (channel IN ('sheena_bdo','sheena_bpi','sheena_psbank')),
    sent_cents INTEGER  NOT NULL CHECK (sent_cents >= 0),
    sent_at    DATETIME NOT NULL,
    sent_by    INTEGER  NOT NULL,
    UNIQUE (cycle_id, channel)
);

-- Audit trail. Every mutation appends one row. actor_telegram_id 0 means the scheduler.
CREATE TABLE events (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at       DATETIME NOT NULL,
    actor_telegram_id INTEGER  NOT NULL,
    actor_name        TEXT     NOT NULL,
    action            TEXT     NOT NULL,
    cycle_id          INTEGER REFERENCES cycles(id),
    bill_id           INTEGER REFERENCES bills(id),
    payable_id        INTEGER REFERENCES payables(id),
    transfer_id       INTEGER,
    before_json       TEXT,
    after_json        TEXT,
    note              TEXT
);
CREATE INDEX events_payable_idx ON events(payable_id, id);
CREATE INDEX events_cycle_idx   ON events(cycle_id, id);
CREATE INDEX events_bill_idx    ON events(bill_id, id);

-- Scheduler idempotency markers, e.g. 'open:2026-10', 'remind:2026-10'.
CREATE TABLE job_runs (
    job_key TEXT PRIMARY KEY,
    ran_at  DATETIME NOT NULL
);

-- +goose Down
DROP TABLE job_runs;
DROP TABLE events;
DROP TABLE transfers;
DROP TABLE payables;
DROP TABLE cycles;
DROP TABLE bills;
DROP TABLE sections;
