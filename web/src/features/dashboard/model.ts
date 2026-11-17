export type Amount = string | null;

export interface Coverage {
  quality: string;
  reasons: string[];
}

export interface Daily {
  date: string;
  openingUsd: Amount;
  closingUsd: Amount;
  assetChangeUsd: Amount;
  netFlowUsd: Amount;
  pnlUsd: Amount;
  returnRate: Amount;
  coverage: Coverage;
  returnMissingReason: string | null;
}

export interface Performance {
  from: string;
  to: string;
  days: Daily[];
  pnlUsd: Amount;
  netFlowUsd: Amount;
  returnRate: Amount;
  coverage: Coverage;
}

export interface Calendar {
  month: string;
  days: Daily[];
  recordedPnlSumUsd: Amount;
  recordedChangeSumUsd: Amount;
  pnlDays: number;
  changeDays: number;
  coverage: Coverage;
}

export interface AllocationGroup {
  key: string;
  name: string;
  valueUsd: Amount;
  share: Amount;
  coverage: Coverage;
}

export interface AllocationAccount {
  id: string;
  name: string;
  totalUsd: Amount;
  wallets: AllocationGroup[];
}

export interface Overview {
  date: string | null;
  totalUsd: Amount;
  pricedSubtotalUsd: Amount;
  coverage: Coverage;
  wallets: AllocationGroup[];
  classes: AllocationGroup[];
  accounts: AllocationAccount[];
}

export interface AccountOption {
  id: string;
  name: string;
}

export interface DashboardSelection {
  account: string;
  range: number;
  month: string;
}

export const wallets = {
  spot: "现货",
  funding: "资金",
  earn: "赚币",
  usdm: "U 本位合约权益",
};

export const assetClasses = { us_equity: "美股", other: "其他资产" };
export const allAccounts = "all";

export function shanghaiDate(date = new Date()) {
  return new Intl.DateTimeFormat("sv-SE", { timeZone: "Asia/Shanghai" }).format(
    date,
  );
}

export function shiftDate(date: string, days: number) {
  const utc = new Date(date + "T00:00:00Z");
  utc.setUTCDate(utc.getUTCDate() + days);
  return utc.toISOString().slice(0, 10);
}

export function shiftMonth(month: string, months: number) {
  const utc = new Date(month + "-01T00:00:00Z");
  utc.setUTCMonth(utc.getUTCMonth() + months);
  return utc.toISOString().slice(0, 7);
}
