import { useEffect, useState, type FormEvent } from "react";
import "./auth.css";

const authPath = "/api/v1/auth";

class AuthError extends Error {
  constructor(readonly status: number) {
    super(String(status));
  }
}

async function request(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
) {
  const response = await fetch(authPath + path, {
    method,
    signal,
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) throw new AuthError(response.status);
  return response;
}

function passkeySupported() {
  return (
    window.isSecureContext &&
    typeof PublicKeyCredential !== "undefined" &&
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === "function" &&
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === "function" &&
    typeof PublicKeyCredential.prototype.toJSON === "function"
  );
}

async function usePasskey() {
  const response = await request("/login/passkey/options", "POST");
  const options: { publicKey: PublicKeyCredentialRequestOptionsJSON } =
    await response.json();
  const publicKey = PublicKeyCredential.parseRequestOptionsFromJSON(
    options.publicKey,
  );
  const credential = await navigator.credentials.get({ publicKey });
  if (!(credential instanceof PublicKeyCredential))
    throw new Error("Missing passkey");
  await request("/login/passkey", "POST", credential.toJSON());
}

async function createPasskey() {
  const response = await request("/passkeys/options", "POST");
  const options: { publicKey: PublicKeyCredentialCreationOptionsJSON } =
    await response.json();
  const publicKey = PublicKeyCredential.parseCreationOptionsFromJSON(
    options.publicKey,
  );
  const created = await navigator.credentials.create({ publicKey });
  if (!(created instanceof PublicKeyCredential))
    throw new Error("Missing passkey");
  await request("/passkeys", "POST", created.toJSON());
}

export function Login({ onLogin }: { onLogin: () => void }) {
  const supported = passkeySupported();
  const [passwordForm, setPasswordForm] = useState(!supported);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function signIn(run: () => Promise<unknown>, password: boolean) {
    setPending(true);
    setError("");
    try {
      await run();
      onLogin();
    } catch (e) {
      if (e instanceof AuthError && e.status === 429) {
        setError("尝试次数过多，请稍后再试。");
      } else if (!password) {
        setError("无法使用这把 Passkey 登录。可以重试或使用账号密码。");
      } else {
        setError(
          e instanceof AuthError && e.status === 401
            ? "用户名或密码错误。"
            : "登录失败，请重试。",
        );
      }
    } finally {
      setPending(false);
    }
  }

  function signInWithPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    void signIn(
      () =>
        request("/login", "POST", {
          username: fields.get("username"),
          password: fields.get("password"),
        }),
      true,
    );
  }

  return (
    <section className="bf-auth bf-login" aria-labelledby="bifrost-login-title">
      <p className="bf-auth-brand">BIFROST</p>
      <h1 id="bifrost-login-title">登录工作区</h1>
      {supported && (
        <button
          className="bf-auth-primary"
          disabled={pending}
          onClick={() => void signIn(usePasskey, false)}
        >
          使用 Passkey 登录
        </button>
      )}
      {supported && !passwordForm && (
        <button
          type="button"
          disabled={pending}
          onClick={() => setPasswordForm(true)}
        >
          使用账号密码
        </button>
      )}
      {passwordForm && (
        <form onSubmit={signInWithPassword}>
          <label>
            用户名
            <input
              name="username"
              required
              maxLength={64}
              autoComplete="username"
            />
          </label>
          <label>
            密码
            <input
              name="password"
              type="password"
              required
              maxLength={1024}
              autoComplete="current-password"
            />
          </label>
          <button
            className={supported ? "" : "bf-auth-primary"}
            disabled={pending}
          >
            登录
          </button>
        </form>
      )}
      {error && (
        <p className="bf-auth-error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

interface Passkey {
  id: string;
  createdAt: string;
  lastUsedAt: string | null;
}

function timeLabel(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export function LoginSecurity({ onLogout }: { onLogout: () => void }) {
  const [items, setItems] = useState<Passkey[] | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [removeID, setRemoveID] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    request("/passkeys", "GET", undefined, controller.signal)
      .then(async (response) => {
        const data: Passkey[] = await response.json();
        if (!controller.signal.aborted) setItems(data);
      })
      .catch((e) => {
        if (controller.signal.aborted) return;
        if (e instanceof AuthError && e.status === 401) onLogout();
        else setError("无法读取已绑定的 Passkey。");
      });
    return () => controller.abort();
  }, [revision, onLogout]);

  async function update(run: () => Promise<unknown>) {
    setPending(true);
    setError("");
    try {
      await run();
      setRemoveID(null);
      setRevision((value) => value + 1);
    } catch (e) {
      if (e instanceof AuthError && e.status === 401) {
        onLogout();
      } else if (e instanceof AuthError && e.status === 428) {
        setError("请退出并重新登录，再修改 Passkey。");
      } else if (e instanceof AuthError && e.status === 409) {
        setError("这把 Passkey 已绑定，或已达到绑定上限。");
      } else {
        setError("操作未能完成，请重试。");
      }
    } finally {
      setPending(false);
    }
  }

  return (
    <section
      className="bf-auth bf-login-security"
      aria-labelledby="login-security-title"
    >
      <h2 id="login-security-title">登录与安全</h2>
      <div className="bf-auth-actions">
        {passkeySupported() && (
          <button
            className="bf-auth-primary"
            disabled={pending}
            onClick={() => void update(createPasskey)}
          >
            添加 Passkey
          </button>
        )}
        <button
          disabled={pending}
          onClick={() =>
            void update(async () => {
              await request("/logout", "POST");
              onLogout();
            })
          }
        >
          退出登录
        </button>
      </div>
      {!passkeySupported() && <p>请在支持 Passkey 的浏览器中添加 Passkey。</p>}
      {items === null && !error && <p role="status">正在读取 Passkey…</p>}
      {items?.length === 0 && <p>尚未绑定 Passkey。</p>}
      {items && items.length > 0 && (
        <ul>
          {items.map((item) => (
            <li key={item.id}>
              <div>
                <strong>Passkey</strong>
                <p>
                  添加于 {timeLabel(item.createdAt)}
                  {item.lastUsedAt &&
                    ` · 最近使用 ${timeLabel(item.lastUsedAt)}`}
                </p>
              </div>
              {removeID === item.id ? (
                <div className="bf-auth-actions">
                  <button
                    disabled={pending}
                    onClick={() =>
                      void update(() =>
                        request(
                          "/passkeys/" + encodeURIComponent(item.id),
                          "DELETE",
                        ),
                      )
                    }
                  >
                    确认移除
                  </button>
                  <button disabled={pending} onClick={() => setRemoveID(null)}>
                    取消
                  </button>
                </div>
              ) : (
                <button disabled={pending} onClick={() => setRemoveID(item.id)}>
                  移除
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {error && (
        <p className="bf-auth-error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
