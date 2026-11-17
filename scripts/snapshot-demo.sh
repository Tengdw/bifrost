#!/usr/bin/env bash
# 每次创建独立库；不读取 .key 或环境中的数据库连接。
set -euo pipefail
cd "$(dirname "$0")/.."
mode=${1:-complete}
case "$mode" in verify|complete|unpriced) ;; *) echo 'usage: snapshot-demo.sh [verify|complete|unpriced]' >&2; exit 2;; esac
pgbin=${PGBIN:-$(pg_config --bindir)}
tmp=$(mktemp -d /tmp/bifrost-snapshot.XXXXXX)
cleanup(){ "$pgbin/pg_ctl" -D "$tmp/data" -m fast stop >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
"$pgbin/initdb" -D "$tmp/data" --auth=trust --username=bifrost --locale=C >/dev/null
"$pgbin/pg_ctl" -w -D "$tmp/data" -o "-h '' -k $tmp" -l "$tmp/server.log" start >/dev/null
export BIFROST_DEMO_DATABASE_URL="postgres://bifrost@/postgres?host=$tmp"
if [[ "$mode" == verify ]]; then
 BIFROST_TEST_DATABASE_URL="$BIFROST_DEMO_DATABASE_URL" go test -count=1 ./...
else
 go run ./cmd/bifrost -seed "$mode"
 export BIFROST_DEMO_SESSION="$(openssl rand -hex 24)"
 echo "本机临时演示口令：$BIFROST_DEMO_SESSION"
 go run ./cmd/bifrost
fi
