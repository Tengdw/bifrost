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
 BIFROST_TEST_DATABASE_URL="$BIFROST_DEMO_DATABASE_URL" go test -count=1 ./cmd/... ./db/... ./internal/...
else
 go run ./cmd/bifrost -seed "$mode"
 go run ./cmd/bifrost -demo-admin >"$tmp/demo-password"
 echo "演示账号 demo，临时密码文件：$tmp/demo-password"
 go run ./cmd/bifrost
fi
