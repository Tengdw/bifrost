import { StrictMode, useCallback, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { Alert, Button, ConfigProvider } from "antd";
import { Workspace } from "./features/holdings/Workspace";
import { Latest } from "./features/holdings/Latest";
import { AccountsPage } from "./features/accounts/AccountsPage";
import { Login, LoginSecurity } from "./features/auth/Auth";
import { DashboardPage } from "./features/dashboard/DashboardPage";
import { bifrostTheme, Masthead } from "./features/dashboard/Dashboard";
import "./style.css";

type Mode =
  "loading" | "login" | "error" | "readonly" | "synthetic" | "snapshot";

const pages = [
  { path: "/", name: "资产分析" },
  { path: "/latest", name: "最新采集" },
  { path: "/accounts", name: "账号管理" },
  { path: "/security", name: "登录安全" },
];

function App() {
  const [mode, setMode] = useState<Mode>("loading");
  const [revision, setRevision] = useState(0);
  const reload = () => {
    setMode("loading");
    setRevision((n) => n + 1);
  };
  const onUnauthorized = useCallback(() => setMode("login"), []);
  const currentPath = location.pathname.replace(/\/$/, "") || "/";
  const visiblePages =
    mode === "snapshot"
      ? pages.filter(({ path }) => path === "/latest" || path === "/security")
      : pages;
  const current =
    visiblePages.find((page) => page.path === currentPath) ?? visiblePages[0];
  const navigation = (
    <nav aria-label="工作台导航" className="mast-nav">
      {visiblePages.map(({ path, name }) => (
        <a
          href={path}
          aria-current={path === current.path ? "page" : undefined}
          key={path}
        >
          {name}
        </a>
      ))}
    </nav>
  );

  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/v1/capabilities", { signal: controller.signal })
      .then(async (response) => {
        if (controller.signal.aborted) return;
        if (response.status === 401) {
          setMode("login");
          return;
        }
        if (!response.ok) {
          setMode("error");
          return;
        }
        const result: unknown = await response.json();
        if (controller.signal.aborted) return;
        if (
          result &&
          typeof result === "object" &&
          "mode" in result &&
          (result.mode === "readonly" ||
            result.mode === "synthetic" ||
            result.mode === "snapshot")
        ) {
          setMode(result.mode);
        } else setMode("error");
      })
      .catch(() => {
        if (!controller.signal.aborted) setMode("error");
      });
    return () => controller.abort();
  }, [revision]);

  if (mode === "login" || mode === "loading" || mode === "error")
    return (
      <ConfigProvider theme={bifrostTheme}>
        <Masthead />
        <main className="frame secondary-page">
          {mode === "login" ? (
            <Login onLogin={reload} />
          ) : mode === "loading" ? (
            <p role="status">正在读取工作区…</p>
          ) : (
            <Alert
              type="error"
              showIcon
              message="工作区读取失败"
              action={
                <Button size="small" onClick={reload}>
                  重新读取
                </Button>
              }
            />
          )}
        </main>
      </ConfigProvider>
    );

  if (current.path === "/" && mode !== "snapshot")
    return (
      <DashboardPage navigation={navigation} onUnauthorized={onUnauthorized} />
    );

  const security = current.path === "/security";
  const accounts = current.path === "/accounts" && mode !== "snapshot";
  return (
    <ConfigProvider theme={bifrostTheme}>
      <Masthead>{navigation}</Masthead>
      <main className="frame secondary-page">
        <h1>{security || accounts ? current.name : "最新采集"}</h1>
        {security ? (
          <LoginSecurity onLogout={onUnauthorized} />
        ) : accounts ? (
          <AccountsPage
            onUnauthorized={onUnauthorized}
            replayAvailable={mode === "readonly"}
          />
        ) : mode === "snapshot" ? (
          <Latest />
        ) : (
          <Workspace />
        )}
      </main>
    </ConfigProvider>
  );
}

const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
