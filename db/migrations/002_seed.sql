-- Seed stock. Quantities are chosen for what the tests have to prove:
-- item_4021 is the brief's example and the one the stress test races, item_5555
-- is scarce enough that a handful of requests sees both success and rejection,
-- item_0000 is already sold out. Items that are absent are the ITEM_NOT_FOUND
-- case, so seeding them would delete the only way to test it.

INSERT INTO
    inventory (item_id, total_stock)
VALUES ('item_4021', 100),
    ('item_4022', 40),
    ('item_5555', 5),
    ('item_0000', 0);