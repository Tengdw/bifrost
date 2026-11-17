ALTER TABLE bifrost.accounts ADD COLUMN exchange text NOT NULL DEFAULT 'binance',
 ADD COLUMN enabled_from timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN disabled_at timestamptz,
 ADD COLUMN last_verified_at timestamptz;
CREATE TABLE bifrost.account_credentials(
 account_id uuid NOT NULL, version integer NOT NULL, ciphertext bytea NOT NULL,
 nonce bytea NOT NULL, master_key_version text NOT NULL, active boolean NOT NULL,
 PRIMARY KEY(account_id,version)
);
CREATE UNIQUE INDEX account_credentials_active ON bifrost.account_credentials(account_id) WHERE active;
