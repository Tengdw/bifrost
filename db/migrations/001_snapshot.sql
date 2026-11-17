-- 本票只处理合成账号的已关闭批次，不包含凭据或交易所接入。
CREATE SCHEMA bifrost;
CREATE TABLE bifrost.accounts (
 id uuid PRIMARY KEY, exchange_uid text NOT NULL UNIQUE, alias text NOT NULL
);
CREATE TABLE bifrost.batches (
 id uuid PRIMARY KEY, account_id uuid NOT NULL, observed_at timestamptz NOT NULL
);
CREATE TABLE bifrost.wallet_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), batch_id uuid NOT NULL, account_id uuid NOT NULL,
 wallet text NOT NULL, UNIQUE(batch_id,account_id,wallet)
);
CREATE TABLE bifrost.holdings (
 run_id uuid NOT NULL, item_key text NOT NULL, asset text NOT NULL, asset_class text NOT NULL,
 underlying_symbol text, quantity numeric NOT NULL, price_usd numeric, usd_value numeric,
 price_source text, priced_at timestamptz, PRIMARY KEY(run_id,item_key)
);
