import { z } from "zod";
import { coverage, decimal, get, group, selection } from "./snapshots";
import type { AllocationGroup, Overview } from "../features/dashboard/model";
import { assetClasses, wallets } from "../features/dashboard/model";

const amount = decimal.nullable();
const walletValues = z.array(
  group.extend({ wallet: z.enum(["spot", "funding", "earn", "usdm"]) }),
);
const classValues = z.array(
  group.extend({ assetClass: z.enum(["us_equity", "other"]) }),
);
const overviewSchema = z.object({
  date: z.iso.date(),
  totalUsd: amount,
  pricedSubtotalUsd: amount,
  coverage,
  accountRows: z.array(
    z.object({
      accountId: z.uuid(),
      totalUsd: amount,
      wallets: walletValues,
    }),
  ),
  allocation: walletValues,
  assetClassAllocation: classValues,
});
const daily = z.object({
  date: z.iso.date(),
  openingUsd: amount,
  closingUsd: amount,
  assetChangeUsd: amount,
  netFlowUsd: amount,
  pnlUsd: amount,
  returnRate: amount,
  coverage,
  returnMissingReason: z.string().nullable(),
});
const window = z.object({
  from: z.iso.date(),
  to: z.iso.date(),
  days: z.array(daily),
  pnlUsd: amount,
  netFlowUsd: amount,
  returnRate: amount,
  coverage,
});
const calendar = z.object({
  month: z.string(),
  days: z.array(daily),
  recordedPnlSumUsd: amount,
  recordedChangeSumUsd: amount,
  pnlDays: z.number().int().nonnegative(),
  changeDays: z.number().int().nonnegative(),
  coverage,
});

function groups(
  entries: (
    z.infer<typeof walletValues>[number] | z.infer<typeof classValues>[number]
  )[],
  names: Record<string, string>,
): AllocationGroup[] {
  return Object.entries(names).map(([key, name]) => {
    const entry = entries.find(
      (value) => ("wallet" in value ? value.wallet : value.assetClass) === key,
    );
    return {
      key,
      name,
      valueUsd: entry?.valueUsd ?? null,
      share: entry?.share ?? null,
      coverage: entry?.coverage ?? { quality: "missing", reasons: [] },
    };
  });
}

export async function dayClose(
  signal: AbortSignal,
  ids: string[],
  names: Map<string, string>,
): Promise<Overview> {
  const query = selection(ids);
  query.set("basis", "day_close");
  const data = await get("overview?" + query, signal, overviewSchema);
  return {
    date: data.date,
    totalUsd: data.totalUsd,
    pricedSubtotalUsd: data.pricedSubtotalUsd,
    coverage: data.coverage,
    wallets: groups(data.allocation, wallets),
    classes: groups(data.assetClassAllocation, assetClasses),
    accounts: data.accountRows.map((account) => ({
      id: account.accountId,
      name: names.get(account.accountId) ?? account.accountId,
      totalUsd: account.totalUsd,
      wallets: groups(account.wallets, wallets),
    })),
  };
}

export function historyWindow(
  signal: AbortSignal,
  ids: string[],
  range: number,
) {
  const query = selection(ids);
  query.set("days", String(range));
  return get("performance?" + query, signal, window);
}

export function historyCalendar(
  signal: AbortSignal,
  ids: string[],
  month: string,
) {
  const query = selection(ids);
  query.set("month", month);
  return get("calendar?" + query, signal, calendar);
}
