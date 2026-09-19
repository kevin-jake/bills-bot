-- +goose Up

-- The sticky note carried more than a name: each card is known by its last four digits, and
-- every card and loan falls due on a fixed day of the month. Both belong to the Bill rather
-- than to any one Cycle, so they are columns on bills and are read live, like the name: a
-- card reissued with new digits should show its new digits on last month's Board too.
ALTER TABLE bills ADD COLUMN card_last4 TEXT
    CHECK (card_last4 IS NULL OR card_last4 GLOB '[0-9][0-9][0-9][0-9]');
ALTER TABLE bills ADD COLUMN due_day INTEGER
    CHECK (due_day IS NULL OR (due_day BETWEEN 1 AND 31));

-- Three cards were seeded under the name the household says out loud; these are the names on
-- the cards themselves, which is what tells two BDO cards or two Mastercards apart.
UPDATE bills SET name = 'Unionbank Mastercard CC' WHERE name = 'UnionBank CC';
UPDATE bills SET name = 'BPI Visa CC'             WHERE name = 'BPI CC';
UPDATE bills SET name = 'HSBC Mastercard CC'      WHERE name = 'HSBC CC';

-- A Bill charged to a card names that card by name, in the standing list and in every
-- Payable snapshot, so a rename has to reach both or a charge points at a card that is gone.
UPDATE bills    SET card_name = 'Unionbank Mastercard CC' WHERE card_name = 'UnionBank CC';
UPDATE bills    SET card_name = 'BPI Visa CC'             WHERE card_name = 'BPI CC';
UPDATE bills    SET card_name = 'HSBC Mastercard CC'      WHERE card_name = 'HSBC CC';
UPDATE payables SET card_name = 'Unionbank Mastercard CC' WHERE card_name = 'UnionBank CC';
UPDATE payables SET card_name = 'BPI Visa CC'             WHERE card_name = 'BPI CC';
UPDATE payables SET card_name = 'HSBC Mastercard CC'      WHERE card_name = 'HSBC CC';

-- Digits and due days as the household confirmed them on 2026-09-20. A loan has no digits.
UPDATE bills SET card_last4 = '2943', due_day = 28 WHERE name = 'Unionbank Mastercard CC';
UPDATE bills SET card_last4 = '5994', due_day =  5 WHERE name = 'BDO JCB CC';
UPDATE bills SET card_last4 = '1630', due_day =  5 WHERE name = 'BDO Unionpay CC';
UPDATE bills SET                      due_day = 25 WHERE name = 'BDO Home Loan';
UPDATE bills SET card_last4 = '1006', due_day = 28 WHERE name = 'RCBC JCB CC';
UPDATE bills SET card_last4 = '8001', due_day = 28 WHERE name = 'RCBC Visa Airmiles';
UPDATE bills SET card_last4 = '7577', due_day = 28 WHERE name = 'BPI Visa CC';
UPDATE bills SET card_last4 = '9361', due_day = 24 WHERE name = 'HSBC Mastercard CC';
UPDATE bills SET                      due_day = 19 WHERE name = 'PSBank Car Loan';
UPDATE bills SET                      due_day = 21 WHERE name = 'Bahay';

-- +goose Down

-- The names go back first, so that 00002's Down still recognises what it seeded.
UPDATE payables SET card_name = 'UnionBank CC' WHERE card_name = 'Unionbank Mastercard CC';
UPDATE payables SET card_name = 'BPI CC'       WHERE card_name = 'BPI Visa CC';
UPDATE payables SET card_name = 'HSBC CC'      WHERE card_name = 'HSBC Mastercard CC';
UPDATE bills    SET card_name = 'UnionBank CC' WHERE card_name = 'Unionbank Mastercard CC';
UPDATE bills    SET card_name = 'BPI CC'       WHERE card_name = 'BPI Visa CC';
UPDATE bills    SET card_name = 'HSBC CC'      WHERE card_name = 'HSBC Mastercard CC';
UPDATE bills    SET name      = 'UnionBank CC' WHERE name = 'Unionbank Mastercard CC';
UPDATE bills    SET name      = 'BPI CC'       WHERE name = 'BPI Visa CC';
UPDATE bills    SET name      = 'HSBC CC'      WHERE name = 'HSBC Mastercard CC';

ALTER TABLE bills DROP COLUMN due_day;
ALTER TABLE bills DROP COLUMN card_last4;
