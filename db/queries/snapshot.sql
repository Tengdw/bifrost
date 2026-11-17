-- name: CreateAccount :exec
INSERT INTO bifrost.accounts (id, exchange_uid, alias) VALUES ($1,$2,$3) ON CONFLICT(id) DO NOTHING;
-- name: CreateBatch :execrows
INSERT INTO bifrost.batches (id,account_id,observed_at) VALUES ($1,$2,$3) ON CONFLICT(id) DO NOTHING;
-- name: CreateRun :one
INSERT INTO bifrost.wallet_runs (batch_id,account_id,wallet) VALUES ($1,$2,$3) RETURNING id;
-- name: CreateHolding :exec
INSERT INTO bifrost.holdings (run_id,item_key,asset,asset_class,underlying_symbol,quantity,price_usd,usd_value,price_source,priced_at)
VALUES ($1,$2,$3,$4,$5,sqlc.arg(quantity_text)::text::numeric,sqlc.narg(price_text)::text::numeric,sqlc.narg(value_text)::text::numeric,$6,$7);
-- name: LoadBatch :one
SELECT id,account_id,observed_at FROM bifrost.batches WHERE (sqlc.narg(batch)::uuid IS NULL OR id=sqlc.narg(batch)) AND (sqlc.narg(as_of)::timestamptz IS NULL OR observed_at<=sqlc.narg(as_of)) ORDER BY observed_at DESC,id DESC LIMIT 1;
-- name: ListWallets :many
SELECT wallet FROM bifrost.wallet_runs WHERE batch_id=$1 AND account_id=$2 ORDER BY wallet;
-- name: ListItems :many
SELECT r.wallet,h.item_key,h.asset,h.asset_class,h.underlying_symbol,h.quantity::text AS quantity_text,COALESCE(h.price_usd::text,'')::text AS price_text
FROM bifrost.wallet_runs r JOIN bifrost.holdings h ON h.run_id=r.id
WHERE r.batch_id=$1 AND r.account_id=$2 ORDER BY r.wallet,h.item_key;
