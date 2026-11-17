"""Local dev and deployment entry points. Never log credentials or source shell files."""
import base64
import hashlib
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import subprocess
import sys
import time
from urllib.parse import quote

ROOT = Path(__file__).resolve().parents[1]


def settings():
    env = dict(os.environ)
    path = ROOT / ".env"
    if path.exists():
        for raw in path.read_text().splitlines():
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            name, sep, value = line.partition("=")
            if not sep or not re.fullmatch(r"[A-Z][A-Z0-9_]*", name.strip()):
                raise ValueError("Invalid .env assignment")
            env.setdefault(name.strip(), value.strip().strip("\"'"))
    return env


def database_url(keyfile, database):
    # The private inventory also contains unrelated entries. Read only the DB block.
    lines = keyfile.read_text().splitlines()
    start = next((i for i, line in enumerate(lines)
                  if line.strip().rstrip(":：") == "数据库配置"), None)
    if start is None or len(lines) < start + 3:
        raise ValueError("Missing database section in BIFROST_KEY_FILE")
    hostport = lines[start + 1].strip()
    user, sep, password = lines[start + 2].strip().partition("/")
    if not re.fullmatch(r"[a-zA-Z0-9.-]+:[0-9]+", hostport) or not sep or not user or not password:
        raise ValueError("Expected host:port and user/password under 数据库配置")
    return f"postgresql://{quote(user, safe='')}:{quote(password, safe='')}@{hostport}/{database}"


def prepare(env, dev):
    database = "bifrost_test" if dev else "bifrost"
    folder = ROOT / ".bifrost" / database
    folder.mkdir(mode=0o700, parents=True, exist_ok=True)
    dsn = database_url(Path(env.get("BIFROST_KEY_FILE", ".key")), database)
    (folder / "database-url").write_text(dsn)
    master = folder / "master-key"
    source = env.get("BIFROST_DEV_MASTER_KEY_FILE" if dev else "MASTER_KEY_FILE")
    if source:
        key = Path(source).read_bytes().strip()
        if len(base64.b64decode(key, validate=True)) != 32:
            raise ValueError("Expected a base64-encoded 32-byte master key")
        if master.exists() and master.read_bytes().strip() != key:
            raise ValueError("Configured master key differs from saved workspace key")
        if not master.exists():
            master.write_bytes(key)
    elif not master.exists():
        with master.open("xb") as stream:
            stream.write(base64.b64encode(secrets.token_bytes(32)))
    env.update(DATABASE_URL_FILE=str(folder / "database-url"), MASTER_KEY_FILE=str(master),
               BIFROST_EXPECT_DATABASE=database, BIFROST_RUNTIME_DIR=str(folder),
               BIFROST_RUNTIME_UID=str(os.getuid()) if os.getuid() else "10001",
               BIFROST_RUNTIME_GID=str(os.getgid()) if os.getuid() else "10001")
    if not dev and os.getuid() == 0:
        for file in (master, folder / "database-url"):
            os.chown(file, 10001, 10001)
    return folder


def sources():
    return {p: p.stat().st_mtime_ns for directory in ("cmd", "internal", "db")
            for p in (ROOT / directory).rglob("*") if p.suffix in (".go", ".sql")} | {
                p: p.stat().st_mtime_ns for p in [ROOT / "go.mod", ROOT / "go.sum"]}


def stop(process):
    if process and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def develop(env, folder, recover=False):
    env.update(BIFROST_LISTEN_ADDR="127.0.0.1:8081", BIFROST_PUBLIC_ORIGIN="http://localhost:5173")
    binary = folder / "bifrost"
    def build():
        return subprocess.run(["go", "build", "-o", binary, "./cmd/bifrost"], env=env).returncode == 0
    def command(*args):
        return [binary, "-mode", "readonly", *args]
    previous = sources()
    if not build():
        raise ValueError("Go build failed")
    subprocess.run(command("-migrate"), env=env, check=True)
    if recover:
        subprocess.run(command("-admin-recover"), env=env, check=True)
        return
    subprocess.run(command("-admin-init"), env=env, check=True)
    vite = ROOT / "web/node_modules/.bin/vite"
    lock_hash = hashlib.sha256((ROOT / "web/package-lock.json").read_bytes()).hexdigest()
    installed = folder / "web-dependencies"
    if not vite.exists() or not installed.exists() or installed.read_text() != lock_hash:
        subprocess.run(["npm", "ci", "--prefix", "web"], env=env, check=True)
        installed.write_text(lock_hash)
    api = frontend = None
    try:
        api = subprocess.Popen(command(), env=env)
        frontend = subprocess.Popen([vite], cwd=ROOT / "web", env=env)
        while api.poll() is None and frontend.poll() is None:
            time.sleep(.7)
            current = sources()
            if previous != current:
                previous = current
                if build():
                    stop(api)
                    subprocess.run(command("-migrate"), env=env, check=True)
                    api = subprocess.Popen(command(), env=env)
        raise ValueError("A development service stopped. See its output above.")
    finally:
        stop(frontend)
        stop(api)


def main(action):
    os.chdir(ROOT)
    os.umask(0o077)
    env = settings()
    dev = action in ("dev", "dev-recover")
    local_go = ROOT / ".bifrost/toolchain/go/bin"
    if dev and not shutil.which("go", path=env.get("PATH")) and (local_go / "go").is_file():
        env["PATH"] = str(local_go) + os.pathsep + env["PATH"]
    if dev and not shutil.which("go", path=env.get("PATH")):
        raise ValueError("make dev requires Go 1.25.7+ on PATH")
    if not dev and not env.get("BIFROST_PUBLIC_ORIGIN", "").startswith("https://"):
        raise ValueError("Set BIFROST_PUBLIC_ORIGIN to the external HTTPS origin in .env")
    folder = prepare(env, dev)
    if dev:
        develop(env, folder, action == "dev-recover")
        return
    def compose(*args):
        subprocess.run(["docker", "compose", *args], check=True, env=env)
    if action == "recover":
        compose("run", "--rm", "app", "-mode", "readonly", "-admin-recover")
        return
    compose("build", "app")
    compose("run", "--rm", "app", "-mode", "readonly", "-migrate")
    compose("run", "--rm", "app", "-mode", "readonly", "-admin-init")
    compose("up", "-d", "app")


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    try:
        main(sys.argv[1])
    except KeyboardInterrupt:
        pass
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
