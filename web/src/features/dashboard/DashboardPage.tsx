import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button } from "antd";
import { accounts, RequestError } from "../../api/snapshots";
import type { Account } from "../../api/snapshots";
import { dayClose, historyCalendar, historyWindow } from "../../api/dashboard";
import { Dashboard } from "./Dashboard";
import { allAccounts, shanghaiDate, shiftDate } from "./model";
import type {
  Calendar,
  DashboardSelection,
  Overview,
  Performance,
} from "./model";

type Load<T> =
  | { state: "ready"; data: T; key: string }
  | { state: "error" | "unauthorized"; key: string };

function observe<T>(
  pending: Promise<T>,
  signal: AbortSignal,
  key: string,
  set: (load: Load<T>) => void,
) {
  pending
    .then((data) => {
      if (!signal.aborted) set({ state: "ready", data, key });
    })
    .catch((error: unknown) => {
      if (!signal.aborted) {
        set({
          state:
            error instanceof RequestError && error.status === 401
              ? "unauthorized"
              : "error",
          key,
        });
      }
    });
}

function current<T>(load: Load<T> | undefined, key: string) {
  return load?.key === key ? load : undefined;
}

function data<T>(load: Load<T> | undefined) {
  return load?.state === "ready" ? load.data : null;
}

export function DashboardPage({
  navigation,
  onUnauthorized,
}: {
  navigation: ReactNode;
  onUnauthorized: () => void;
}) {
  const latestDate = shiftDate(shanghaiDate(), -1);
  const maxMonth = latestDate.slice(0, 7);
  const [selection, setSelection] = useState<DashboardSelection>({
    account: allAccounts,
    range: 30,
    month: maxMonth,
  });
  const [revision, setRevision] = useState(0);
  const [accountResult, setAccounts] = useState<Load<Account[]>>();
  const [overview, setOverview] = useState<Load<Overview>>();
  const [performance, setPerformance] = useState<Load<Performance>>();
  const [calendar, setCalendar] = useState<Load<Calendar>>();

  const accountId = selection.account;
  const { range, month } = selection;
  const dayKey = [accountId, revision].join("/");
  const performanceKey = [dayKey, range].join("/");
  const calendarKey = [dayKey, month].join("/");
  const currentAccounts = current(accountResult, String(revision));
  const currentOverview = current(overview, dayKey);
  const currentPerformance = current(performance, performanceKey);
  const currentCalendar = current(calendar, calendarKey);
  const accountList = data(currentAccounts);
  const activeAccounts =
    accountList?.filter(
      (account) => account.id === accountId || accountId === allAccounts,
    ) ?? [];
  const minMonth = activeAccounts.reduce((earliest, account) => {
    const start = shanghaiDate(new Date(account.enabledFrom)).slice(0, 7);
    return start < earliest ? start : earliest;
  }, maxMonth);

  useEffect(() => {
    const controller = new AbortController();
    observe(
      accounts(controller.signal),
      controller.signal,
      String(revision),
      setAccounts,
    );
    return () => controller.abort();
  }, [revision]);

  useEffect(() => {
    if (!accountList) return;
    const controller = new AbortController();
    const ids = accountId === allAccounts ? [] : [accountId];
    const names = new Map(
      accountList.map((account) => [account.id, account.alias]),
    );
    observe(
      dayClose(controller.signal, ids, names),
      controller.signal,
      dayKey,
      setOverview,
    );
    return () => controller.abort();
  }, [accountId, accountList, dayKey]);

  useEffect(() => {
    const controller = new AbortController();
    observe(
      historyWindow(
        controller.signal,
        accountId === allAccounts ? [] : [accountId],
        range,
      ),
      controller.signal,
      performanceKey,
      setPerformance,
    );
    return () => controller.abort();
  }, [accountId, range, performanceKey]);

  useEffect(() => {
    const controller = new AbortController();
    observe(
      historyCalendar(
        controller.signal,
        accountId === allAccounts ? [] : [accountId],
        month,
      ),
      controller.signal,
      calendarKey,
      setCalendar,
    );
    return () => controller.abort();
  }, [accountId, month, calendarKey]);

  const results = [
    currentAccounts,
    currentOverview,
    currentPerformance,
    currentCalendar,
  ];
  const unauthorized = results.some(
    (result) => result?.state === "unauthorized",
  );
  useEffect(() => {
    if (unauthorized) onUnauthorized();
  }, [unauthorized, onUnauthorized]);

  const problems = [
    [currentAccounts, "账号列表"],
    [currentOverview, "日终估值"],
    [currentPerformance, "收益走势"],
    [currentCalendar, "日历"],
  ] as const;
  const failed = problems
    .filter(([result]) => result?.state === "error")
    .map(([, label]) => label);
  const pending = !failed.length && results.some((result) => !result);

  return (
    <Dashboard
      navigation={navigation}
      notices={
        failed.length > 0 ? (
          <Alert
            className="dashboard-notice"
            type="error"
            showIcon
            message={failed.join("、") + "读取失败"}
            action={
              <Button size="small" onClick={() => setRevision((n) => n + 1)}>
                重新读取
              </Button>
            }
          />
        ) : pending ? (
          <p className="dashboard-notice" role="status">
            正在读取看板…
          </p>
        ) : undefined
      }
      selection={selection}
      onSelect={setSelection}
      accounts={(accountList ?? []).map((account) => ({
        id: account.id,
        name: account.alias + (account.disabledAt ? "（已停用）" : ""),
      }))}
      overview={data(currentOverview)}
      performance={data(currentPerformance)}
      calendar={data(currentCalendar)}
      minMonth={minMonth}
      latestDate={latestDate}
    />
  );
}
