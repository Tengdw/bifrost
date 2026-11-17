CREATE TABLE IF NOT EXISTS bifrost.current_batches (
 id uuid PRIMARY KEY, requested_at timestamptz NOT NULL, accounts jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS bifrost.current_runs (
 batch_id uuid NOT NULL, account_id uuid NOT NULL, wallet text NOT NULL,
 state text NOT NULL, reason text, observed_at timestamptz, payload jsonb,
 PRIMARY KEY(batch_id,account_id,wallet)
);
CREATE INDEX IF NOT EXISTS current_batch_latest ON bifrost.current_batches(requested_at DESC);
CREATE TABLE IF NOT EXISTS bifrost.current_items (
 batch_id uuid NOT NULL,account_id uuid NOT NULL,wallet text NOT NULL,item_key text NOT NULL,
 asset text NOT NULL,asset_class text NOT NULL,underlying_symbol text,quantity numeric NOT NULL,
 usd_value numeric,price_usd numeric,price_bid numeric,price_ask numeric,quote_info jsonb,
 PRIMARY KEY(batch_id,account_id,wallet,item_key)
);
CREATE TABLE IF NOT EXISTS bifrost.current_positions (
 batch_id uuid NOT NULL,account_id uuid NOT NULL,wallet text NOT NULL,symbol text NOT NULL,side text NOT NULL,
 quantity numeric NOT NULL,margin_asset text NOT NULL,entry_price numeric NOT NULL,mark_price numeric NOT NULL,
 unrealized numeric NOT NULL,notional numeric NOT NULL,PRIMARY KEY(batch_id,account_id,wallet,symbol,side)
);
