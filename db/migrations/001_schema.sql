CREATE TABLE inventory (
    item_id text NOT NULL,
    total_stock integer NOT NULL,
    reserved_stock integer NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_pkey PRIMARY KEY (item_id),
    CONSTRAINT inventory_total_known CHECK (total_stock >= 0),
    CONSTRAINT inventory_reserved_known CHECK (reserved_stock >= 0),
    CONSTRAINT inventory_never_overreserved CHECK (reserved_stock <= total_stock)
);

CREATE SEQUENCE reservation_id_seq;

CREATE TABLE reservations (
    reservation_id text NOT NULL,
    item_id text NOT NULL,
    user_id text NOT NULL,
    quantity integer NOT NULL,
    status text NOT NULL DEFAULT 'active',
    idempotency_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    confirmed_at timestamptz,
    CONSTRAINT reservations_pkey PRIMARY KEY (reservation_id),
    CONSTRAINT reservations_quantity_positive CHECK (quantity > 0),
    CONSTRAINT reservations_status_known CHECK (
        status IN (
            'active',
            'confirmed',
            'expired'
        )
    ),
    CONSTRAINT reservations_item_fkey FOREIGN KEY (item_id) REFERENCES inventory (item_id)
);

CREATE INDEX reservations_expiry_queue ON reservations (expires_at)
WHERE
    status = 'active';

CREATE UNIQUE INDEX reservations_idempotency ON reservations (user_id, idempotency_key)
WHERE
    idempotency_key IS NOT NULL;

CREATE VIEW live_reservation_totals AS
SELECT i.item_id, i.total_stock, i.reserved_stock, COALESCE(SUM(r.quantity), 0)::integer AS live_reserved
FROM
    inventory i
    LEFT JOIN reservations r ON r.item_id = i.item_id
    AND r.status = 'active'
    AND r.expires_at > now()
GROUP BY
    i.item_id,
    i.total_stock,
    i.reserved_stock;