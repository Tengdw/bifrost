import { useState } from "react";
import { createRoot } from "react-dom/client";
import { Dashboard } from "../../web/src/features/dashboard/Dashboard";
import { allAccounts, wallets } from "../../web/src/features/dashboard/model";
import type {
  AllocationGroup,
  Calendar,
  Daily,
  DashboardSelection,
  Overview,
  Performance,
} from "../../web/src/features/dashboard/model";
import { accounts, series, names, mix, start, end, date } from "./data";

const coverage = { quality: "complete", reasons: [] };
const accountsList = names.map((name, index) => ({ id: String(index), name }));
const amount = (cents: number) => (cents / 100).toFixed(2);

function allocation(values: number[], total: number): AllocationGroup[] {
  return Object.entries(wallets).map(([key, name], index) => ({
    key,
    name,
    valueUsd: amount(values[index]),
    share: String(values[index] / total),
    coverage,
  }));
}

function App() {
  const [selection, onSelect] = useState<DashboardSelection>({
    account: allAccounts,
    range: 30,
    month: date(end).slice(0, 7),
  });
  const ids =
    selection.account === allAccounts ? [0, 1] : [Number(selection.account)];
  const rows = series(ids);
  const close = rows.at(-1)!.close;
  const values = ids.map((id) => {
    const closing = accounts[id].at(-1)!.close;
    const grouped = mix[id]
      .slice(0, 3)
      .map((fraction) => Math.round(closing * fraction));
    return [...grouped, closing - grouped.reduce((sum, n) => sum + n, 0)];
  });
  const overview: Overview = {
    date: date(end),
    totalUsd: amount(close),
    pricedSubtotalUsd: amount(close),
    coverage,
    wallets: allocation(
      values[0].map((_, i) => values.reduce((total, row) => total + row[i], 0)),
      close,
    ),
    classes: [],
    accounts: ids.map((id, index) => {
      const closing = accounts[id].at(-1)!.close;
      return {
        id: String(id),
        name: names[id],
        totalUsd: amount(closing),
        wallets: allocation(values[index], closing),
      };
    }),
  };

  function daily(row: (typeof rows)[number]): Daily {
    return {
      date: date(row.day),
      openingUsd: amount(row.open),
      closingUsd: amount(row.close),
      assetChangeUsd: amount(row.delta),
      netFlowUsd: amount(row.flow),
      pnlUsd: amount(row.pnl),
      returnRate: String(row.rate),
      coverage,
      returnMissingReason: null,
    };
  }

  const selected = rows.slice(-selection.range);
  const monthRows = rows.filter((row) =>
    date(row.day).startsWith(selection.month),
  );
  const performance: Performance = {
    from: date(selected[0].day),
    to: date(end),
    days: selected.map(daily),
    pnlUsd: amount(selected.reduce((sum, row) => sum + row.pnl, 0)),
    netFlowUsd: amount(selected.reduce((sum, row) => sum + row.flow, 0)),
    returnRate: String(
      selected.reduce((growth, row) => growth * (1 + row.rate), 1) - 1,
    ),
    coverage,
  };
  const calendar: Calendar = {
    month: selection.month,
    days: monthRows.map(daily),
    recordedPnlSumUsd: amount(monthRows.reduce((sum, row) => sum + row.pnl, 0)),
    recordedChangeSumUsd: amount(
      monthRows.reduce((sum, row) => sum + row.delta, 0),
    ),
    pnlDays: monthRows.length,
    changeDays: monthRows.length,
    coverage,
  };

  return (
    <Dashboard
      preview
      accounts={accountsList}
      minMonth={date(start).slice(0, 7)}
      latestDate={date(end)}
      selection={selection}
      onSelect={onSelect}
      overview={overview}
      performance={performance}
      calendar={calendar}
    />
  );
}

const root = document.getElementById("root");
if (root) createRoot(root).render(<App />);
