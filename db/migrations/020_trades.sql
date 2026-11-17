CREATE INDEX trade_observation ON bifrost.current_runs(account_id,wallet,observed_at)
 WHERE state IN ('succeeded','partial');

CREATE TABLE bifrost.trade_coverage (
 account_id uuid NOT NULL, wallet text NOT NULL,
 started_at timestamptz NOT NULL, through_at timestamptz, reason text,
 PRIMARY KEY (account_id,wallet)
);
CREATE TABLE bifrost.exchange_orders (
 account_id uuid NOT NULL, wallet text NOT NULL, symbol text NOT NULL, order_id bigint NOT NULL,
 base_asset text NOT NULL, quote_asset text NOT NULL, side text NOT NULL, position_side text NOT NULL,
 status text NOT NULL, order_type text NOT NULL, orig_quantity numeric NOT NULL, executed_quantity numeric NOT NULL,
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, preexisting boolean NOT NULL, used_sor boolean NOT NULL,
 PRIMARY KEY (account_id,wallet,symbol,order_id,updated_at,status,executed_quantity)
);
CREATE INDEX exchange_order_interval ON bifrost.exchange_orders(account_id,wallet,updated_at);
CREATE TABLE bifrost.exchange_fills (
 account_id uuid NOT NULL, wallet text NOT NULL, symbol text NOT NULL, trade_id bigint NOT NULL, is_allocation boolean NOT NULL,
 order_id bigint NOT NULL, traded_at timestamptz NOT NULL,
 base_asset text NOT NULL, quote_asset text NOT NULL, side text NOT NULL, position_side text NOT NULL,
 quantity numeric NOT NULL, quote_quantity numeric NOT NULL, price numeric NOT NULL,
 fee numeric NOT NULL, fee_asset text NOT NULL,
 PRIMARY KEY (account_id,wallet,symbol,trade_id,order_id,is_allocation)
);
CREATE INDEX exchange_fill_interval ON bifrost.exchange_fills(account_id,wallet,traded_at);
