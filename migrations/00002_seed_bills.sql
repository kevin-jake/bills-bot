-- +goose Up

-- The sticky note this bot replaces, as it stood when the bot was built. From here on the
-- standing list is managed in-bot; this migration only saves the household from typing it
-- in once. Amounts are deliberately absent: a Payable opens unknown every Cycle.

INSERT INTO sections (id, name, display_order) VALUES
    (1, 'UnionBank',  1),
    (2, 'BDO',        2),
    (3, 'RCBC',       3),
    (4, 'BPI',        4),
    (5, 'HSBC',       5),
    (6, 'PSBank',     6),
    (7, 'Bahay',      7),
    (8, 'Investment', 8),
    (9, 'Utilities',  9);

-- Only Internet PLDT is charged to a card. The two investments are paid by Kevin in cash,
-- so they are kevin_direct and count toward Cash out like any other bill he settles.
INSERT INTO bills (name, section_id, channel, card_name, display_order) VALUES
    ('UnionBank CC',       1, 'kevin_direct',    NULL,                 1),
    ('BDO JCB CC',         2, 'sheena_bdo',      NULL,                 1),
    ('BDO Unionpay CC',    2, 'sheena_bdo',      NULL,                 2),
    ('BDO Home Loan',      2, 'sheena_bdo',      NULL,                 3),
    ('RCBC JCB CC',        3, 'sheena_bpi',      NULL,                 1),
    ('RCBC Visa Airmiles', 3, 'sheena_bpi',      NULL,                 2),
    ('BPI CC',             4, 'sheena_bpi',      NULL,                 1),
    ('HSBC CC',            5, 'sheena_bpi',      NULL,                 1),
    ('PSBank Car Loan',    6, 'sheena_psbank',   NULL,                 1),
    ('Bahay',              7, 'sheena_bpi',      NULL,                 1),
    ('BPI Investment',     8, 'kevin_direct',    NULL,                 1),
    ('BPI Wealth Builder', 8, 'kevin_direct',    NULL,                 2),
    ('Internet PLDT',      9, 'charged_to_card', 'RCBC Visa Airmiles', 1),
    ('Batelec',            9, 'kevin_direct',    NULL,                 2),
    ('Water',              9, 'kevin_direct',    NULL,                 3),
    ('GCash funds',        9, 'kevin_direct',    NULL,                 4);

-- +goose Down

-- Removes only what was seeded, by name, so that Bills added in-bot survive a rollback.
DELETE FROM bills WHERE name IN (
    'UnionBank CC', 'BDO JCB CC', 'BDO Unionpay CC', 'BDO Home Loan', 'RCBC JCB CC',
    'RCBC Visa Airmiles', 'BPI CC', 'HSBC CC', 'PSBank Car Loan', 'Bahay',
    'BPI Investment', 'BPI Wealth Builder', 'Internet PLDT', 'Batelec', 'Water', 'GCash funds');
DELETE FROM sections WHERE id BETWEEN 1 AND 9;
