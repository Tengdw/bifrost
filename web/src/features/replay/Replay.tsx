import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import {
  Alert,
  AutoComplete,
  Button,
  Descriptions,
  Input,
  Select,
  Table,
} from "antd";
import { replayDetail, replayOptions } from "../../api/replay";
import type {
  Direction,
  Market,
  ReplayDetail,
  ReplayOptions,
  ReplayQuery,
} from "../../api/replay";
import { RequestError } from "../../api/snapshots";
import type { Account } from "../../api/snapshots";
import { shanghaiDate, shiftDate } from "../dashboard/model";

const time = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  dateStyle: "short",
  timeStyle: "medium",
  hour12: false,
});
const sides: Record<string, string> = {
  BUY: "买入",
  SELL: "卖出",
  BOTH: "净仓",
  LONG: "多仓",
  SHORT: "空仓",
};
const states = {
  pending: "待采集",
  collecting: "采集中",
  covered: "已读取",
  partial: "覆盖不完整",
};
const orderStates: Record<string, string> = {
  NEW: "已委托",
  PARTIALLY_FILLED: "部分成交",
  FILLED: "已成交",
  CANCELED: "已撤单",
  REJECTED: "已拒绝",
  EXPIRED: "已过期",
  EXPIRED_IN_MATCH: "撮合过期",
};

function interval(from: string, to: string, today: string) {
  if (!from || !to || from > to || to > today || to > shiftDate(from, 89))
    return null;
  return {
    from: from + "T00:00:00+08:00",
    to:
      to === today
        ? new Date().toISOString()
        : shiftDate(to, 1) + "T00:00:00+08:00",
  };
}

function dateTime(value: string | null | undefined) {
  return value ? time.format(new Date(value)) : "—";
}

type TimelineEntry =
  | {
      kind: "order";
      phase: "placed" | "opening" | "updated";
      at: string;
      data: ReplayDetail["orders"][number];
    }
  | { kind: "fill"; at: string; data: ReplayDetail["fills"][number] };

export function Replay({
  account,
  onUnauthorized,
}: {
  account: Account;
  onUnauthorized: () => void;
}) {
  const today = shanghaiDate();
  const [wallet, setWallet] = useState<Market>("spot");
  const [asset, setAsset] = useState("");
  const [positionSide, setPositionSide] = useState<Direction>("BOTH");
  const [from, setFrom] = useState(shiftDate(today, -6));
  const [to, setTo] = useState(today);
  const [catalog, setCatalog] = useState<{
    key: string;
    options: ReplayOptions;
  }>();
  const [catalogError, setCatalogError] = useState(false);
  const [query, setQuery] = useState<ReplayQuery>();
  const [result, setResult] = useState<{
    query: ReplayQuery;
    data: ReplayDetail;
  }>();
  const [failure, setFailure] = useState<{
    query: ReplayQuery;
    message: string;
  }>();
  const [validation, setValidation] = useState("");
  const heading = useRef<HTMLHeadingElement>(null);
  const catalogKey = [wallet, from, to].join("/");

  useEffect(() => {
    heading.current?.focus();
  }, []);

  useEffect(() => {
    const dates = interval(from, to, today);
    if (!dates) return;
    const controller = new AbortController();
    replayOptions(controller.signal, account.id, wallet, dates)
      .then((options) => {
        if (!controller.signal.aborted) {
          setCatalog({ key: catalogKey, options });
          setCatalogError(false);
        }
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof RequestError && error.status === 401)
          onUnauthorized();
        else setCatalogError(true);
      });
    return () => controller.abort();
  }, [account.id, wallet, from, to, today, catalogKey, onUnauthorized]);

  useEffect(() => {
    if (!query) return;
    const controller = new AbortController();
    replayDetail(controller.signal, query)
      .then((data) => {
        if (!controller.signal.aborted) setResult({ query, data });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof RequestError && error.status === 401)
          onUnauthorized();
        else
          setFailure({
            query,
            message:
              error instanceof RequestError &&
              error.code === "interval_too_large"
                ? "交易记录过多，请缩短时间段。"
                : "持仓变动读取失败，请重新查询。",
          });
      });
    return () => controller.abort();
  }, [query, onUnauthorized]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const code = asset.trim().toUpperCase();
    if (!code) {
      setValidation("请输入资产或合约交易对。");
      return;
    }
    if (!/^[\p{L}\p{N}_.-]{1,50}$/u.test(code)) {
      setValidation("请输入有效的资产或合约交易对。");
      return;
    }
    const dates = interval(from, to, today);
    if (!dates) {
      setValidation("请选择有效的起止日期，最多连续 90 天。");
      return;
    }
    setValidation("");
    setQuery({
      accountId: account.id,
      wallet,
      asset: code,
      positionSide,
      ...dates,
    });
  }

  const options = catalog?.key === catalogKey ? catalog.options : undefined;
  const data = result?.query === query ? result?.data : undefined;
  const failureMessage =
    failure?.query === query ? failure?.message : undefined;

  return (
    <section className="replay">
      <h2 ref={heading} tabIndex={-1}>
        {account.alias} · 持仓变动
      </h2>
      <p>选择资产和北京时间日期，查看真实采集之间的订单、成交与余额核对。</p>
      <form className="controls" onSubmit={submit}>
        <label>
          钱包
          <Select
            aria-label="钱包"
            value={wallet}
            onChange={(next) => {
              setWallet(next);
              setAsset("");
              setPositionSide("BOTH");
              setQuery(undefined);
              setValidation("");
              setCatalogError(false);
            }}
            options={[
              { value: "spot", label: "现货" },
              { value: "usdm", label: "U 本位合约" },
            ]}
          />
        </label>
        <label>
          {wallet === "spot" ? "资产" : "合约交易对"}
          <AutoComplete
            aria-label={wallet === "spot" ? "资产" : "合约交易对"}
            value={asset}
            onChange={setAsset}
            options={[
              ...new Set((options?.assets ?? []).map((item) => item.asset)),
            ].map((value) => ({ value }))}
            placeholder={wallet === "spot" ? "例如 BTC" : "例如 BTCUSDT"}
            filterOption={(input, option) =>
              option?.value.includes(input.toUpperCase()) ?? false
            }
          />
        </label>
        {wallet === "usdm" && (
          <label>
            仓位方向
            <Select
              aria-label="仓位方向"
              value={positionSide}
              onChange={setPositionSide}
              options={(["BOTH", "LONG", "SHORT"] as const).map((value) => ({
                value,
                label: sides[value],
              }))}
            />
          </label>
        )}
        <label>
          开始日期{" "}
          <Input
            type="date"
            required
            max={to}
            value={from}
            onChange={(event) => setFrom(event.target.value)}
          />
        </label>
        <label>
          结束日期{" "}
          <Input
            type="date"
            required
            min={from}
            max={today}
            value={to}
            onChange={(event) => setTo(event.target.value)}
          />
        </label>
        <Button
          type="primary"
          htmlType="submit"
          loading={Boolean(query && !data && !failureMessage)}
        >
          查询持仓变动
        </Button>
      </form>
      {catalogError && (
        <p role="status">资产目录读取失败，可直接输入资产查询。</p>
      )}
      {validation && <Alert type="warning" message={validation} />}
      {failureMessage && <Alert type="error" message={failureMessage} />}
      {data && result && <ReplayResult data={data} query={result.query} />}
    </section>
  );
}

function ReplayResult({
  data,
  query,
}: {
  data: ReplayDetail;
  query: ReplayQuery;
}) {
  const { coverage } = data;
  const timeline: TimelineEntry[] = [];
  const placements = new Set<string>();
  const from = Date.parse(query.from);
  const to = Date.parse(query.to);
  for (const order of data.orders) {
    const id = order.symbol + "/" + order.id;
    const created = Date.parse(order.createdAt);
    const updated = Date.parse(order.updatedAt);
    if (
      !order.preexisting &&
      created >= from &&
      created < to &&
      !placements.has(id)
    ) {
      timeline.push({
        kind: "order",
        at: order.createdAt,
        phase: "placed",
        data: order,
      });
      placements.add(id);
    }
    if (updated < from && order.preexisting) {
      timeline.push({
        kind: "order",
        at: coverage.startedAt ?? query.from,
        phase: "opening",
        data: order,
      });
    } else if (
      updated >= from &&
      updated < to &&
      (updated !== created || order.status !== "NEW")
    ) {
      timeline.push({
        kind: "order",
        at: order.updatedAt,
        phase: "updated",
        data: order,
      });
    }
  }
  for (const fill of data.fills)
    timeline.push({ kind: "fill", at: fill.at, data: fill });
  timeline.sort((left, right) => Date.parse(left.at) - Date.parse(right.at));

  return (
    <div aria-live="polite">
      <h3>
        {query.asset}
        {query.wallet === "usdm" ? " · " + sides[query.positionSide] : ""}
      </h3>
      <Descriptions size="small" bordered column={{ xs: 1, sm: 2 }}>
        <Descriptions.Item label="采集状态">
          {states[coverage.state]}
        </Descriptions.Item>
        <Descriptions.Item label="记录范围">
          {dateTime(coverage.startedAt)} 至 {dateTime(coverage.through)}
        </Descriptions.Item>
        <Descriptions.Item label="起点持仓">
          {data.opening
            ? data.opening.quantity + " · " + dateTime(data.opening.observedAt)
            : "—"}
        </Descriptions.Item>
        <Descriptions.Item label="终点持仓">
          {data.closing
            ? data.closing.quantity + " · " + dateTime(data.closing.observedAt)
            : "—"}
        </Descriptions.Item>
        <Descriptions.Item label="已解释变化">
          {data.explainedChange ?? "—"}
        </Descriptions.Item>
        <Descriptions.Item label="待核对差额">
          {data.unexplainedDifference ?? "—"}
        </Descriptions.Item>
      </Descriptions>
      {coverage.reason && (
        <Alert
          className="replay-alert"
          type="warning"
          message={coverage.reason}
        />
      )}
      <p className="note">
        核对基于真实采集。差额可能来自充提、划转或尚未覆盖的交易。
      </p>
      <Table
        scroll={{ x: 910 }}
        size="small"
        rowKey={(row) =>
          row.kind === "fill"
            ? ["fill", row.data.symbol, row.data.id].join("/")
            : [
                row.phase,
                row.data.symbol,
                row.data.id,
                row.data.updatedAt,
                row.data.status,
                row.data.executedQuantity,
              ].join("/")
        }
        dataSource={timeline}
        locale={{
          emptyText:
            coverage.state === "covered"
              ? "所选时段没有已记录的订单或成交"
              : "所选时段暂无可用的订单或成交",
        }}
        columns={[
          {
            title: "北京时间",
            render: (_value: unknown, row: TimelineEntry) => dateTime(row.at),
          },
          {
            title: "事件",
            render: (_value: unknown, row: TimelineEntry) =>
              row.kind === "fill"
                ? "成交"
                : row.phase === "opening"
                  ? "起点时已挂单"
                  : row.phase === "placed"
                    ? "下单"
                    : "订单状态",
          },
          {
            title: "交易对",
            render: (_value: unknown, row: TimelineEntry) => row.data.symbol,
          },
          {
            title: "订单号",
            render: (_value: unknown, row: TimelineEntry) =>
              row.kind === "order" ? row.data.id : row.data.orderId,
          },
          {
            title: "方向",
            render: (_value: unknown, row: TimelineEntry) =>
              [
                sides[row.data.side] ?? row.data.side,
                row.data.positionSide ? sides[row.data.positionSide] : null,
              ]
                .filter(Boolean)
                .join(" · "),
          },
          {
            title: "数量",
            render: (_value: unknown, row: TimelineEntry) => row.data.quantity,
          },
          {
            title: "状态 / 成交价",
            render: (_value: unknown, row: TimelineEntry) =>
              row.kind === "fill"
                ? row.data.price
                : row.phase === "placed"
                  ? row.data.type
                  : (orderStates[row.data.status] ?? row.data.status) +
                    " · 已成交 " +
                    row.data.executedQuantity,
          },
          {
            title: "手续费",
            render: (_value: unknown, row: TimelineEntry) =>
              row.kind === "fill"
                ? row.data.fee + " " + row.data.feeAsset
                : "—",
          },
          {
            title: "持仓变化",
            render: (_value: unknown, row: TimelineEntry) =>
              row.kind === "fill" ? row.data.delta : "—",
          },
          {
            title: "按成交推算持仓",
            render: (_value: unknown, row: TimelineEntry) =>
              row.kind === "fill" ? (row.data.balanceAfter ?? "—") : "—",
          },
        ]}
      />
    </div>
  );
}
