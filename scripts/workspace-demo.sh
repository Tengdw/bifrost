#!/usr/bin/env bash
# 无需个人凭据：临时数据库、固定合成行情、仅监听回环地址。
set -euo pipefail
cd "$(dirname "$0")/.."
umask 077
pgbin=${PGBIN:-$(pg_config --bindir)}
tmp=$(mktemp -d /tmp/bifrost-workspace.XXXXXX)
cleanup(){ if [[ -n ${app_pid:-} ]]; then kill "$app_pid" 2>/dev/null || true; wait "$app_pid" 2>/dev/null || true; fi; "$pgbin/pg_ctl" -D "$tmp/data" -m fast stop >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
"$pgbin/initdb" -D "$tmp/data" --auth=trust --username=bifrost --locale=C >/dev/null
"$pgbin/pg_ctl" -w -D "$tmp/data" -o "-h '' -k $tmp" -l "$tmp/server.log" start >/dev/null
export DATABASE_URL_FILE="$tmp/db" MASTER_KEY_FILE="$tmp/master"
echo "postgres://bifrost@/postgres?host=$tmp" >"$DATABASE_URL_FILE"
openssl rand -base64 32 >"$MASTER_KEY_FILE"

go build -o "$tmp/bifrost" ./cmd/bifrost
"$tmp/bifrost" -mode synthetic -migrate
"$tmp/bifrost" -mode synthetic -demo-admin >"$tmp/demo-password"
echo "演示账号 demo，临时密码文件：$tmp/demo-password"
port=${BIFROST_PORT:-8080}
if [[ ${1:-serve} == verify-browser ]]; then
 export BIFROST_PUBLIC_ORIGIN="http://localhost:$port"
 "$tmp/bifrost" -mode synthetic -port "$port" & app_pid=$!
 for _ in {1..50}; do if curl -fs --max-time 1 "$BIFROST_PUBLIC_ORIGIN/latest" >/dev/null; then break; fi; sleep 0.1; done
 BIFROST_SMOKE_URL="$BIFROST_PUBLIC_ORIGIN" BIFROST_SMOKE_PASSWORD_FILE="$tmp/demo-password" npm --prefix web run test:browser
else
 "$tmp/bifrost" -mode synthetic -port "$port"
fi
