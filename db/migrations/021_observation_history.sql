ALTER TABLE bifrost.current_batches
 ADD COLUMN day_close_at timestamptz;
ALTER TABLE bifrost.current_runs
 ADD COLUMN quantity_complete boolean NOT NULL DEFAULT false;

CREATE UNIQUE INDEX current_day_close_boundary
 ON bifrost.current_batches(day_close_at) WHERE day_close_at IS NOT NULL;
