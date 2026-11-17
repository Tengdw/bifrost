import { useState } from "react";
import type { ReactNode } from "react";
import { Button, ConfigProvider, Modal, Segmented, Select, Table } from "antd";
import { graphic } from "echarts/core";
import { Chart } from "./Chart";
import type { ChartOption } from "./Chart";
import { money, numberClass, percent, signed, usd } from "./format";
import { allAccounts, shiftDate, shiftMonth, wallets } from "./model";
import type {
  AccountOption,
  Calendar,
  Daily,
  DashboardSelection,
  Overview,
  Performance,
} from "./model";
import brand from "./brand.png";
import "./dashboard.css";

const tones = ["#ca9a40", "#476e68", "#84a99b", "#c1c9c4"];
const metrics = [
  { label: "资产走势", key: "closingUsd", format: usd },
  { label: "每日收益额", key: "pnlUsd", format: signed },
  { label: "每日收益率", key: "returnRate", format: percent },
] as const;

export const bifrostTheme = {
  token: {
    colorPrimary: "#386d5e",
    borderRadius: 8,
    fontFamily:
      '-apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif',
    colorText: "#23332d",
  },
};

export function Masthead({ children }: { children?: ReactNode }) {
  return (
    <header>
      <div className="frame mast">
        <a className="brand" href="./" aria-label="Bifrost 资产工作台">
          <img src={brand} alt="" />
          Bifrost
          <span className="divider" />
          <span className="workspace">资产工作台</span>
        </a>
        {children}
      </div>
    </header>
  );
}

interface Props {
  selection: DashboardSelection;
  onSelect: (selection: DashboardSelection) => void;
  accounts: AccountOption[];
  overview: Overview | null;
  performance: Performance | null;
  calendar: Calendar | null;
  minMonth: string;
  latestDate: string;
  notices?: ReactNode;
  navigation?: ReactNode;
  preview?: boolean;
}

export function Dashboard({
  selection,
  onSelect,
  accounts,
  overview,
  performance,
  calendar,
  minMonth,
  latestDate,
  notices,
  navigation,
  preview = false,
}: Props) {
  const { account, range, month } = selection;
  const maxMonth = latestDate.slice(0, 7);
  const [metric, setMetric] = useState(0);
  const [calendarMode, setCalendarMode] = useState<"每日盈亏" | "资产变化">(
    "每日盈亏",
  );
  const [selectedDay, setSelectedDay] = useState<string | null>(null);
  const [accountDetails, setAccountDetails] = useState(false);
  const [allocationMode, setAllocationMode] = useState<"按钱包" | "按资产类别">(
    "按钱包",
  );

  const calendarMetric =
    calendarMode === "每日盈亏" ? "pnlUsd" : "assetChangeUsd";
  const monthly =
    (calendarMode === "每日盈亏"
      ? calendar?.recordedPnlSumUsd
      : calendar?.recordedChangeSumUsd) ?? null;
  const [year, monthNumber] = month.split("-").map(Number);
  const monthStart = new Date(month + "-01T00:00:00Z");
  const offset = (monthStart.getUTCDay() + 6) % 7;
  const daysInMonth = Number(
    shiftDate(shiftMonth(month, 1) + "-01", -1).slice(-2),
  );
  const days = new Map(calendar?.days.map((row) => [row.date, row]));
  const active = selectedDay ? days.get(selectedDay) : undefined;
  const view = metrics[metric];
  const dates =
    performance?.days.map((row) => row.date.slice(5).replace("-", "/")) ?? [];
  const values = performance?.days.map((row) => row[view.key]) ?? [];

  const line: ChartOption = {
    color: ["#327765"],
    grid: { left: 5, top: 16, bottom: 3, right: 14, containLabel: true },
    tooltip: {
      trigger: "axis",
      renderMode: "richText",
      confine: true,
      formatter: (params) => {
        const first = Array.isArray(params) ? params[0] : params;
        const index = first?.dataIndex;
        if (index === undefined) return "";
        return (
          dates[index] + "\n" + view.label + "　" + view.format(values[index])
        );
      },
    },
    xAxis: {
      type: "category",
      boundaryGap: false,
      data: dates,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: "#73817d",
        fontSize: 11,
        margin: 12,
        interval: Math.ceil(range / 5) - 1,
        showMaxLabel: true,
        hideOverlap: true,
      },
    },
    yAxis: {
      type: "value",
      scale: metric === 0,
      splitNumber: 4,
      axisLabel: {
        color: "#73817d",
        fontSize: 10,
        formatter: (value: number) =>
          metric === 2
            ? percent(String(value), 1, false)
            : Math.abs(value) >= 1000
              ? money(
                  String(value / 1000),
                  Number.isInteger(value / 1000) ? 0 : 1,
                ) + "k"
              : money(String(value), 0),
      },
      splitLine: { lineStyle: { type: "dashed", color: "#e6eae6" } },
    },
    series: [
      {
        type: "line",
        data: values.map((value) => (value === null ? null : Number(value))),
        symbolSize: 5,
        showSymbol:
          values.length === 1 || values.some((value) => value === null),
        lineStyle: { width: 2.8 },
        areaStyle:
          metric === 0
            ? {
                color: new graphic.LinearGradient(0, 0, 0, 1, [
                  { offset: 0, color: "#90c3ad80" },
                  { offset: 1, color: "#e8f0e700" },
                ]),
              }
            : undefined,
      },
    ],
  };

  const groups =
    (allocationMode === "按钱包" ? overview?.wallets : overview?.classes) ?? [];
  const hasDistribution = groups.some(
    (group) => group.share !== null && Number(group.share) > 0,
  );
  const donut: ChartOption = {
    tooltip: {
      trigger: "item",
      renderMode: "richText",
      confine: true,
      formatter: (params) => {
        const entry =
          groups[
            Array.isArray(params) ? params[0].dataIndex : params.dataIndex
          ];
        return entry
          ? entry.name +
              "\n" +
              usd(entry.valueUsd) +
              " · " +
              percent(entry.share, 1, false)
          : "";
      },
    },
    series: [
      {
        type: "pie",
        radius: ["75%", "96%"],
        label: { show: false },
        itemStyle: { borderWidth: 3, borderColor: "#fff", borderRadius: 4 },
        data: groups.map((group, index) => ({
          name: group.name,
          value: group.share === null ? undefined : Number(group.share),
          itemStyle: { color: tones[index % tones.length] },
        })),
      },
    ],
  };

  function moveMonth(delta: number) {
    setSelectedDay(null);
    onSelect({ ...selection, month: shiftMonth(month, delta) });
  }

  return (
    <ConfigProvider theme={bifrostTheme}>
      <Masthead>
        {preview ? (
          <span className="demo">
            <i />
            示例数据
          </span>
        ) : (
          navigation
        )}
      </Masthead>
      <main className="frame">
        <div className="intro">
          <div>
            <small>PORTFOLIO ANALYTICS</small>
            <h1>资产分析</h1>
            <p>查看账户权益、收益表现与资产配置。</p>
          </div>
          <div className="filters">
            <label>
              账户范围
              <Select
                aria-label="账户范围"
                value={account}
                onChange={(value) => {
                  setSelectedDay(null);
                  onSelect({ ...selection, account: value });
                }}
                options={[
                  { value: allAccounts, label: "全部账号" },
                  ...accounts.map(({ id, name }) => ({
                    value: id,
                    label: name,
                  })),
                ]}
              />
            </label>
            <span>
              计价币种 <b>USD</b>
            </span>
          </div>
        </div>
        {notices}
        <section className="top">
          <article className="hero">
            <small>PORTFOLIO VALUE</small>
            <div className="heroName">
              当前资产总值 <span>USD</span>
            </div>
            <strong className="worth">{usd(overview?.totalUsd ?? null)}</strong>
            <div className="period">
              <span>近 {range} 天收益额</span>
              <b>
                {signed(performance?.pnlUsd ?? null)} <em>USD</em>
              </b>
            </div>
            <div className="period">
              <span>区间收益率</span>
              <b>{percent(performance?.returnRate ?? null)}</b>
            </div>
            <div className="stamp">
              <i />
              {overview?.date
                ? "截至 " + overview.date.replaceAll("-", "/") + " · 日终估值"
                : "日终估值"}
            </div>
            {overview?.totalUsd === null && (
              <p className="hero-reason">
                {overview.coverage.reasons.join("；") || "暂无完整日终估值"}
                {overview.pricedSubtotalUsd !== null && (
                  <> · 已估值小计 {usd(overview.pricedSubtotalUsd)}</>
                )}
              </p>
            )}
          </article>
          <article className="panel trends">
            <div className="sectionHead">
              <div>
                <small>01 / PERFORMANCE</small>
                <h2>收益表现</h2>
              </div>
              <Segmented
                aria-label="时间范围"
                value={range}
                onChange={(value) => onSelect({ ...selection, range: value })}
                options={[7, 30, 90].map((value) => ({
                  value,
                  label: value + " 天",
                }))}
              />
            </div>
            <div className="metricTabs">
              {metrics.map(({ label }, index) => (
                <button
                  key={label}
                  aria-pressed={metric === index}
                  className={metric === index ? "active" : ""}
                  onClick={() => setMetric(index)}
                >
                  {label}
                </button>
              ))}
              <span>{metric === 2 ? "%" : "USD"}</span>
            </div>
            <div className="line">
              {values.every((value) => value === null) ? (
                <div className="chart-empty">
                  {performance
                    ? metric === 0
                      ? "暂无日终数据"
                      : "暂无可靠收益数据"
                    : "—"}
                </div>
              ) : (
                <Chart
                  option={line}
                  label={view.label + "，最近 " + range + " 天"}
                />
              )}
            </div>
            <div className="lineFoot">
              <span>
                {dates.length ? dates[0] + " — " + dates.at(-1) : "—"}
              </span>
              <span>
                区间净流入　{signed(performance?.netFlowUsd ?? null)} USD
              </span>
            </div>
          </article>
        </section>
        <section className="bottom">
          <article className="panel calendar">
            <div className="sectionHead">
              <div>
                <small>02 / CALENDAR</small>
                <h2>每日盈亏日历</h2>
              </div>
              <Segmented
                aria-label="日历指标"
                size="small"
                value={calendarMode}
                onChange={setCalendarMode}
                options={["每日盈亏", "资产变化"]}
              />
            </div>
            <div className="monthbar">
              <div>
                <Button
                  size="small"
                  aria-label="上个月"
                  disabled={month <= minMonth}
                  onClick={() => moveMonth(-1)}
                >
                  ‹
                </Button>
                <b>
                  {year} 年 {monthNumber} 月
                </b>
                <Button
                  size="small"
                  aria-label="下个月"
                  disabled={month >= maxMonth}
                  onClick={() => moveMonth(1)}
                >
                  ›
                </Button>
              </div>
              <span>
                已记录日合计{" "}
                <strong className={numberClass(monthly)}>
                  {signed(monthly)} USD
                </strong>
              </span>
            </div>
            <div className="days">
              {"一二三四五六日".split("").map((week) => (
                <div className="weekday" key={week}>
                  周{week}
                </div>
              ))}
              {Array.from({ length: offset }, (_, index) => (
                <div key={"blank" + index} />
              ))}
              {Array.from({ length: daysInMonth }, (_, index) => {
                const day = month + "-" + String(index + 1).padStart(2, "0");
                const row = day <= latestDate ? days.get(day) : undefined;
                const amount = row?.[calendarMetric] ?? null;
                return (
                  <button
                    key={day}
                    className={
                      "day " + (amount === null ? "empty" : numberClass(amount))
                    }
                    disabled={!row}
                    onClick={() => setSelectedDay(day)}
                    aria-label={
                      day +
                      " " +
                      calendarMode +
                      " " +
                      (amount === null ? "无数据" : signed(amount) + " USD")
                    }
                  >
                    <span>{index + 1}</span>
                    <b>{signed(amount, 0)}</b>
                  </button>
                );
              })}
            </div>
            <p className="note">
              按北京时间结算 · 日期格显示整美元，点击可查看明细。
            </p>
          </article>
          <article className="panel allocation">
            <div className="sectionHead">
              <div>
                <small>03 / ALLOCATION</small>
                <h2>资产分布</h2>
              </div>
              {preview ? (
                <span className="pill">按钱包</span>
              ) : (
                <Select
                  size="small"
                  aria-label="资产分布分组"
                  popupMatchSelectWidth={false}
                  variant="borderless"
                  className="pill allocation-control"
                  value={allocationMode}
                  onChange={setAllocationMode}
                  options={["按钱包", "按资产类别"].map((value) => ({
                    value,
                    label: value,
                  }))}
                />
              )}
            </div>
            <div className="ring">
              {hasDistribution ? (
                <Chart
                  option={donut}
                  label={allocationMode + "的美元估值占比"}
                />
              ) : (
                <div className="ring-empty" />
              )}
              <div className="center">
                <small>
                  {allocationMode === "按钱包" ? "钱包类别" : "资产类别"}
                </small>
                <b>
                  {hasDistribution
                    ? String(groups.length).padStart(2, "0")
                    : "—"}
                </b>
              </div>
            </div>
            <div>
              {groups.map((group, index) => (
                <div
                  className="legend"
                  key={group.key}
                  title={group.coverage.reasons.join("；")}
                >
                  <i style={{ background: tones[index % tones.length] }} />
                  <span>{group.name}</span>
                  <b>{usd(group.valueUsd)}</b>
                  <em>{percent(group.share, 1, false)}</em>
                </div>
              ))}
            </div>
            <button className="detail" onClick={() => setAccountDetails(true)}>
              查看账号分布 <span>↗</span>
            </button>
          </article>
        </section>
        <footer>
          <span>Bifrost / 资产分析</span>
          <span>{preview ? "示例估值" : "日终估值"} · USD · UTC+8</span>
        </footer>
      </main>
      <Modal
        open={Boolean(active)}
        title={active ? active.date + " · 每日明细" : ""}
        footer={null}
        onCancel={() => setSelectedDay(null)}
      >
        {active && <DailyDetail daily={active} preview={preview} />}
      </Modal>
      <Modal
        open={accountDetails}
        title="账号资产分布 · USD"
        footer={null}
        width={760}
        onCancel={() => setAccountDetails(false)}
      >
        <Table
          pagination={false}
          size="small"
          scroll={{ x: 600 }}
          rowKey="id"
          dataSource={overview?.accounts ?? []}
          locale={{ emptyText: "暂无日终数据" }}
          columns={[
            { title: "账号", dataIndex: "name" },
            ...Object.entries(wallets).map(([key, title]) => ({
              title,
              render: (_: unknown, row: Overview["accounts"][number]) =>
                money(
                  row.wallets.find((wallet) => wallet.key === key)?.valueUsd ??
                    null,
                ),
            })),
            {
              title: "总值",
              dataIndex: "totalUsd",
              render: (value: string | null) => money(value),
            },
          ]}
        />
      </Modal>
    </ConfigProvider>
  );
}

function DailyDetail({ daily, preview }: { daily: Daily; preview: boolean }) {
  const reason = [
    ...new Set(
      [...daily.coverage.reasons, daily.returnMissingReason].filter(Boolean),
    ),
  ].join("；");
  const values = [
    ["期初资产", usd(daily.openingUsd)],
    ["净流入", signed(daily.netFlowUsd)],
    ["资产变化", signed(daily.assetChangeUsd)],
    ["当日盈亏", signed(daily.pnlUsd)],
    ["当日收益率", percent(daily.returnRate)],
    ["期末资产", usd(daily.closingUsd)],
  ];
  return (
    <>
      <p className="modalNote">
        {preview
          ? "金额均以美元计价，本日外部资金流按日初发生。"
          : "金额均以美元计价。"}
      </p>
      {values.map(([label, value]) => (
        <div className="kv" key={label}>
          <span>{label}</span>
          <b>{value}</b>
        </div>
      ))}
      {reason && <p className="modalNote">{reason}</p>}
    </>
  );
}
