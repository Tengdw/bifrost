import { useEffect, useState } from "react";
import { Alert, Button, Segmented, Table } from "antd";
import { latest, RequestError } from "../../api/snapshots";
import type { Group } from "../../api/snapshots";
const names: Record<string, string> = {
  spot: "现货钱包",
  funding: "资金钱包",
  earn: "赚币钱包",
  usdm: "U 本位合约",
  us_equity: "美股",
  other: "其他资产",
};
function amount(value: string | null) {
  if (value === null) return "—";
  const [whole, fraction] = value.split(".");
  return (
    whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",") +
    (fraction ? "." + fraction : "")
  );
}
function percentage(ratio: string | null) {
  if (ratio === null) return "—";
  const [whole, digits = ""] = ratio.split(".");
  const fraction = digits.padEnd(2, "0");
  const rest = fraction.slice(2).replace(/0+$/, "");
  return (
    (whole + fraction.slice(0, 2)).replace(/^0+(?=\d)/, "") +
    (rest ? "." + rest : "") +
    "%"
  );
}
type State =
  | { kind: "loading" | "login" | "error" | "empty" }
  | { kind: "ready"; data: Awaited<ReturnType<typeof latest>> };
export function Latest() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [attempt, retry] = useState(0);
  const [mode, setMode] = useState("按钱包");
  useEffect(() => {
    const controller = new AbortController();
    setState({ kind: "loading" });
    latest(controller.signal)
      .then((data) => setState({ kind: "ready", data }))
      .catch((e: unknown) => {
        if (!controller.signal.aborted)
          setState({
            kind:
              e instanceof RequestError && e.status === 401
                ? "login"
                : e instanceof RequestError && e.status === 404
                  ? "empty"
                  : "error",
          });
      });
    return () => controller.abort();
  }, [attempt]);
  if (state.kind === "login")
    return (
      <section>
        <h2>打开本地演示</h2>
        <form method="post" action="/session">
          <label>
            演示会话口令{" "}
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
  if (state.kind !== "ready")
    return (
      <section aria-live="polite">
        <p>
          {state.kind === "loading"
            ? "正在读取同批持仓…"
            : state.kind === "empty"
              ? "暂无已采集批次"
              : "读取失败，请重试。"}
        </p>
        {state.kind !== "loading" && (
          <Button onClick={() => retry(attempt + 1)}>重新读取</Button>
        )}
      </section>
    );
  const { overview: o, items } = state.data;
  const wallets = mode === "按钱包";
  const groups = wallets
    ? o.allocation.map((g) => ({ ...g, key: g.wallet }))
    : o.assetClassAllocation.map((g) => ({ ...g, key: g.assetClass }));
  return (
    <>
      <p>
        合成账号 · 观察时间{" "}
        {new Date(o.asOf).toLocaleString("zh-CN", {
          timeZone: "Asia/Shanghai",
          hour12: false,
        })}
        （北京时间）
      </p>
      <section className="metrics">
        <div>
          组合总额（USD）<strong>{amount(o.totalUsd)}</strong>
        </div>
        <div>
          已估值小计（USD）<strong>{amount(o.pricedSubtotalUsd)}</strong>
        </div>
      </section>
      {o.coverage.reasons.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message={o.coverage.reasons.join("；")}
        />
      )}
      <section>
        <h2>资产分布</h2>
        <Segmented
          value={mode}
          options={["按钱包", "按资产类别"]}
          onChange={setMode}
        />
        <Table
          pagination={false}
          scroll={{ x: 600 }}
          dataSource={groups}
          columns={[
            {
              title: "分组",
              dataIndex: "key",
              render: (k: string) => names[k],
            },
            {
              title: wallets ? "钱包总额（USD）" : "类别总额（USD）",
              dataIndex: "valueUsd",
              render: amount,
            },
            {
              title: "已估值小计（USD）",
              dataIndex: "pricedSubtotalUsd",
              render: amount,
            },
            { title: "占比", dataIndex: "share", render: percentage },
            {
              title: "说明",
              render: (_: unknown, r: Group) =>
                r.coverage.reasons.join("；") || "完整",
            },
          ]}
        />
      </section>
      <section>
        <h2>同批持仓明细</h2>
        <Table
          rowKey={(i) => [i.accountId, i.wallet, i.itemKey].join("/")}
          pagination={false}
          scroll={{ x: 440 }}
          dataSource={items}
          locale={{ emptyText: "已确认空仓" }}
          columns={[
            { title: "资产", dataIndex: "asset" },
            { title: "数量", dataIndex: "quantity" },
            { title: "估值（USD）", dataIndex: "valueUsd", render: amount },
            {
              title: "钱包",
              dataIndex: "wallet",
              render: (k: string) => names[k],
            },
          ]}
        />
      </section>
    </>
  );
}
