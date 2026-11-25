BEGIN;

ALTER TABLE events
    ALTER COLUMN description SET DEFAULT '',
    ALTER COLUMN description SET NOT NULL,
    ADD COLUMN organizer_id uuid NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN status text NOT NULL DEFAULT 'DRAFT',
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN price_minor bigint NOT NULL,
    ADD COLUMN currency text NOT NULL,
    DROP COLUMN price,
    ADD CONSTRAINT events_seat_counts_valid
        CHECK (total_seats >= 0 AND available_seats BETWEEN 0 AND total_seats),
    ADD CONSTRAINT events_status_valid
        CHECK (status IN ('DRAFT', 'PUBLISHED', 'CANCELLED')),
    ADD CONSTRAINT events_version_positive
        CHECK (version > 0),
    ADD CONSTRAINT events_price_minor_positive
        CHECK (price_minor > 0),
    ADD CONSTRAINT events_currency_valid
        CHECK (currency = 'RUB');

ALTER TABLE bookings
    DROP CONSTRAINT bookings_event_id_fkey,
    ADD CONSTRAINT bookings_event_id_fkey
        FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE RESTRICT;

COMMIT;