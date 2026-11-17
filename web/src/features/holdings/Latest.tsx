import { Positions } from "./Positions";
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
  | { kind: "loading" | "running" | "login" | "error" | "empty" }
  | { kind: "ready"; data: Awaited<ReturnType<typeof latest>> };
export function Latest({
  accountIds = [],
  revision = 0,
  managed = false,
}: {
  accountIds?: string[];
  revision?: number;
  managed?: boolean;
}) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [attempt, retry] = useState(0);
  const [mode, setMode] = useState("按钱包");
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    setState({ kind: "loading" });
    latest(controller.signal, accountIds)
      .then((data) => {
        if (!controller.signal.aborted) setState({ kind: "ready", data });
      })
      .catch((e: unknown) => {
        if (
          !controller.signal.aborted &&
          e instanceof RequestError &&
          e.code === "batch_running"
        ) {
          setState({ kind: "running" });
          timer = setTimeout(() => retry((n) => n + 1), 1000);
          return;
        }
        if (!controller.signal.aborted)
          setState({
            kind:
              e instanceof RequestError && e.status === 401
                ? "login"
                : e instanceof RequestError && e.missingSnapshot
                  ? "empty"
                  : "error",
          });
      });
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [attempt, accountIds.join(","), revision]);
  if (state.kind !== "ready")
    return (
      <section aria-live="polite">
        <p>
          {state.kind === "running"
            ? "正在采集，结束后自动读取…"
            : state.kind === "loading"
              ? "正在读取同批持仓…"
              : state.kind === "empty"
                ? "暂无已采集批次"
                : state.kind === "login"
                  ? "会话已过期，请重新登录。"
                  : "读取失败，请重试。"}
        </p>
        {state.kind !== "loading" && state.kind !== "running" && (
          <Button
            onClick={() =>
              state.kind === "login"
                ? window.location.reload()
                : retry(attempt + 1)
            }
          >
            {state.kind === "login" ? "重新登录" : "重新读取"}
          </Button>
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
        {managed ? "所选账号" : "合成账号"} · 观察时间{" "}
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
            ...(groups.some((g) => g.share !== null)
              ? [{ title: "占比", dataIndex: "share", render: percentage }]
              : []),
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
          locale={{
            emptyText:
              o.coverage.quality === "complete"
                ? "已确认空仓"
                : "本批持仓未观察或不完整，请查看缺失原因",
          }}
          columns={[
            { title: "账号", dataIndex: "accountId" },
            { title: "资产", dataIndex: "asset" },
            { title: "数量", dataIndex: "quantity" },
            { title: "估值（USD）", dataIndex: "valueUsd", render: amount },
            {
              title: "钱包",
              dataIndex: "wallet",
              render: (k: string) => names[k],
            },
            { title: "观察时间", dataIndex: "observedAt" },
            {
              title: "合约模式",
              render: (_: unknown, row: (typeof items)[number]) =>
                row.mode || row.components?.mode || "—",
            },
            {
              title: "价格证据",
              render: (_: unknown, row: (typeof items)[number]) =>
                row.price
                  ? row.price.priceUsd +
                    " USD · " +
                    row.price.source +
                    " · " +
                    row.price.pricedAt +
                    " · " +
                    row.price.path +
                    (row.price.tradingDay
                      ? " · 交易日 " + row.price.tradingDay
                      : "") +
                    (row.price.verifiedClose ? " · 已核验收盘价" : "") +
                    (row.price.bidPrice ? " · 买 " + row.price.bidPrice : "") +
                    (row.price.askPrice ? " · 卖 " + row.price.askPrice : "")
                  : row.coverage.reasons.join("；") || "—",
            },
            {
              title: "组成",
              render: (_: unknown, row: (typeof items)[number]) =>
                Object.entries(row.components || {})
                  .map(([k, v]) => k + ": " + v)
                  .join("；"),
            },
          ]}
        />
      </section>
      {o.staleWallets.length > 0 && (
        <section>
          <h2>上次观察（仅供参考）</h2>
          <p>以下旧值不计入本批总额或已估值小计。</p>
          <Table
            rowKey={(r) => r.accountId + "/" + r.wallet}
            dataSource={o.staleWallets}
            pagination={false}
            scroll={{ x: 700 }}
            columns={[
              { title: "账号", dataIndex: "accountId" },
              {
                title: "钱包",
                dataIndex: "wallet",
                render: (v: string) => names[v],
              },
              { title: "旧观察时间", dataIndex: "observedAt" },
              { title: "旧估值（USD）", dataIndex: "valueUsd", render: amount },
              { title: "原因", dataIndex: "reason" },
              {
                title: "持仓",
                render: (_: unknown, r: (typeof o.staleWallets)[number]) =>
                  r.items
                    .map(
                      (i) =>
                        i.asset +
                        " " +
                        i.quantity +
                        " · " +
                        amount(i.valueUsd) +
                        " USD",
                    )
                    .join("；"),
              },
              {
                title: "仓位",
                render: (_: unknown, r: (typeof o.staleWallets)[number]) =>
                  r.positions
                    .map(
                      (p) =>
                        p.symbol +
                        " " +
                        p.quantity +
                        " · 名义价值 " +
                        amount(p.notional),
                    )
                    .join("；"),
              },
            ]}
          />
        </section>
      )}
      {managed && (
        <Positions
          accountIds={accountIds}
          batchId={o.batchId}
          complete={o.coverage.quality === "complete"}
        />
      )}
    </>
  );
}
