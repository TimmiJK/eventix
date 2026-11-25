BEGIN;

ALTER TABLE bookings
    DROP CONSTRAINT bookings_event_id_fkey,
    ADD CONSTRAINT bookings_event_id_fkey
        FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE;

ALTER TABLE events
    ADD COLUMN price numeric(10, 2);

UPDATE events
SET price = price_minor::numeric / 100;

ALTER TABLE events
    ALTER COLUMN price SET NOT NULL,
    ALTER COLUMN description DROP NOT NULL,
    ALTER COLUMN description DROP DEFAULT,
    DROP CONSTRAINT events_seat_counts_valid,
    DROP CONSTRAINT events_status_valid,
    DROP CONSTRAINT events_version_positive,
    DROP CONSTRAINT events_price_minor_positive,
    DROP CONSTRAINT events_currency_valid,
    DROP COLUMN organizer_id,
    DROP COLUMN status,
    DROP COLUMN version,
    DROP COLUMN price_minor,
    DROP COLUMN currency;

COMMIT;