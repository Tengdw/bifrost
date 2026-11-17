import React, { useState, useEffect } from "react";
import { Workspace } from "./features/holdings/Workspace";
import { createRoot } from "react-dom/client";
import { Latest } from "./features/holdings/Latest";
import "./style.css";
function App() {
  const [mode, setMode] = useState("loading");
  useEffect(() => {
    const c = new AbortController();
    fetch("/api/v1/capabilities", { signal: c.signal })
      .then(async (r) => {
        if (r.status === 401) {
          setMode("login");
          return;
        }
        if (r.status === 404) {
          setMode("snapshot");
          return;
        }
        if (r.ok) {
          const data: unknown = await r.json();
          if (
            data &&
            typeof data === "object" &&
            "mode" in data &&
            (data.mode === "readonly" || data.mode === "synthetic")
          )
            setMode(data.mode);
          else setMode("error");
        } else setMode("error");
      })
      .catch(() => {
        if (!c.signal.aborted) setMode("error");
      });
    return () => c.abort();
  }, []);
  if (mode === "loading") return <p>正在读取工作区…</p>;
  if (mode === "error")
    return (
      <section>
        <p>工作区读取失败，请重试。</p>
        <button onClick={() => location.reload()}>重新读取</button>
      </section>
    );
  if (mode === "login")
    return (
      <section>
        <h2>进入工作区</h2>
        <form method="post" action="/session">
          <label>
            会话口令{" "}
            <input
              required
              type="password"
              name="token"
              autoComplete="current-password"
            />
          </label>{" "}
          <button>进入</button>
        </form>
      </section>
    );
  return mode === "snapshot" ? <Latest /> : <Workspace />;
}
const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <React.StrictMode>
      <main>
        <header>
          <small>BIFROST</small>
          <h1>最新采集</h1>
        </header>
        <App />
      </main>
    </React.StrictMode>,
  );
