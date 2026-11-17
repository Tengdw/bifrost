#!/usr/bin/env bash
# 无需个人凭据：临时数据库、固定合成行情、仅监听回环地址。
set -euo pipefail
cd "$(dirname "$0")/.."
umask 077
pgbin=${PGBIN:-$(pg_config --bindir)}
tmp=$(mktemp -d /tmp/bifrost-workspace.XXXXXX)
cleanup(){ "$pgbin/pg_ctl" -D "$tmp/data" -m fast stop >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
"$pgbin/initdb" -D "$tmp/data" --auth=trust --username=bifrost --locale=C >/dev/null
"$pgbin/pg_ctl" -w -D "$tmp/data" -o "-h '' -k $tmp" -l "$tmp/server.log" start >/dev/null
export DATABASE_URL_FILE="$tmp/db" MASTER_KEY_FILE="$tmp/master" SESSION_TOKEN_FILE="$tmp/session"
echo "postgres://bifrost@/postgres?host=$tmp" >"$DATABASE_URL_FILE"
openssl rand -base64 32 >"$MASTER_KEY_FILE"
openssl rand -hex 32 >"$SESSION_TOKEN_FILE"
go build -o "$tmp/bifrost" ./cmd/bifrost
"$tmp/bifrost" -mode synthetic -migrate
echo "本机临时登录口令文件：$SESSION_TOKEN_FILE"
"$tmp/bifrost" -mode synthetic -port "${BIFROST_PORT:-8080}"
