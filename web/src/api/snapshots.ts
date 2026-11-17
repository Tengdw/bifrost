import { z } from "zod";
const decimal = z.string().regex(/^-?[0-9]+(\.[0-9]+)?$/);
const coverage = z.object({
  quality: z.string(),
  reasons: z.array(z.string()),
});
const group = z.object({
  valueUsd: decimal.nullable(),
  pricedSubtotalUsd: decimal.nullable(),
  share: decimal.nullable(),
  coverage,
});
export type Group = z.infer<typeof group>;
const wallet = z.enum(["spot", "funding", "earn", "usdm"]);
const assetClass = z.enum(["us_equity", "other"]);
const overviewSchema = z.object({
  batchId: z.uuid(),
  asOf: z.iso.datetime({ offset: true }),
  totalUsd: decimal.nullable(),
  pricedSubtotalUsd: decimal.nullable(),
  allocation: z.array(group.extend({ wallet })),
  assetClassAllocation: z.array(group.extend({ assetClass })),
  coverage,
});
const holding = z.object({
  accountId: z.uuid(),
  wallet,
  itemKey: z.string(),
  asset: z.string(),
  assetClass,
  quantity: decimal,
  valueUsd: decimal.nullable(),
  observedAt: z.string().optional(),
  components: z.record(z.string(), z.string()).optional(),
  price: z
    .object({
      priceUsd: decimal,
      source: z.string(),
      pricedAt: z.string(),
      path: z.string(),
    })
    .nullish(),
  coverage,
});
type Holding = z.infer<typeof holding>;
const pageSchema = z.object({
  items: z.array(holding),
  nextCursor: z.string().nullable(),
});
export class RequestError extends Error {
  constructor(
    readonly status: number,
    readonly missingSnapshot: boolean,
  ) {
    super("读取失败");
  }
}
async function get<T>(
  path: string,
  signal: AbortSignal,
  schema: z.ZodType<T>,
): Promise<T> {
  const r = await fetch("/api/v1/" + path, { signal });
  if (!r.ok)
    throw new RequestError(
      r.status,
      r.status === 404 &&
        path.startsWith("overview?") &&
        (await r.json()).code === "no_snapshot",
    );
  return schema.parse(await r.json());
}
export async function latest(signal: AbortSignal, accountIds: string[] = []) {
  const query = selection(accountIds);
  query.set("basis", "latest");
  const overview = await get("overview?" + query, signal, overviewSchema);
  const items: Holding[] = [];
  const q = selection(accountIds);
  q.set("batchId", overview.batchId);
  do {
    const page = await get("holdings?" + q, signal, pageSchema);
    items.push(...page.items);
    if (!page.nextCursor) break;
    q.set("cursor", page.nextCursor);
  } while (true);
  return { overview, items };
}

const accountSchema = z.object({
  id: z.uuid(),
  alias: z.string(),
  exchange: z.string(),
  enabledFrom: z.string(),
  disabledAt: z.string().nullable(),
});
export type Account = z.infer<typeof accountSchema>;
export interface Credentials {
  apiKey: string;
  apiSecret: string;
}
export interface BindAccount extends Credentials {
  alias: string;
}
async function mutate<T>(
  path: string,
  method: string,
  body: unknown,
  schema: z.ZodType<T>,
): Promise<T> {
  const response = await fetch("/api/v1/" + path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new RequestError(response.status, false);
  return schema.parse(await response.json());
}
export function accounts(signal: AbortSignal) {
  return get("accounts", signal, z.array(accountSchema));
}
export function bindAccount(body: BindAccount) {
  return mutate("accounts", "POST", body, accountSchema);
}
export function renameAccount(id: string, alias: string) {
  return mutate("accounts/" + id, "PATCH", { alias }, accountSchema);
}
export function disableAccount(id: string) {
  return mutate("accounts/" + id, "PATCH", { enabled: false }, accountSchema);
}
export function rotateAccount(id: string, body: Credentials) {
  return mutate("accounts/" + id + "/credentials", "PUT", body, accountSchema);
}
const syncItem = z.object({
  batchId: z.uuid(),
  accountId: z.uuid(),
  wallet,
  state: z.enum(["started", "already_running"]),
});
const syncStatus = z.object({
  batchId: z.uuid(),
  accountId: z.uuid(),
  wallet,
  state: z.enum([
    "running",
    "succeeded",
    "partial",
    "failed",
    "interrupted",
    "skipped",
  ]),
  lastSuccessAt: z.string().nullable(),
  reason: z.string().nullable(),
});
export type SyncStatus = z.infer<typeof syncStatus>;
export type Wallet = z.infer<typeof wallet>;
export function refresh(accountIds: string[], wallets?: Wallet[]) {
  return mutate(
    "refresh",
    "POST",
    { accountIds, ...(wallets ? { wallets } : {}) },
    z.array(syncItem),
  );
}
export function sync(batchId: string, signal: AbortSignal) {
  return get("sync/" + batchId, signal, z.array(syncStatus));
}
const position = z.object({
  accountId: z.uuid(),
  symbol: z.string(),
  side: z.string(),
  quantity: decimal,
  marginAsset: z.string(),
  entryPrice: decimal.nullable(),
  markPrice: decimal.nullable(),
  unrealized: decimal.nullable(),
  notional: decimal.nullable(),
  observedAt: z.string(),
});
export async function positions(
  signal: AbortSignal,
  accountIds: string[],
  batchId: string,
) {
  const q = selection(accountIds);
  q.set("batchId", batchId);
  const items: z.infer<typeof position>[] = [];
  do {
    const page = await get(
      "positions?" + q,
      signal,
      z.object({ items: z.array(position), nextCursor: z.string().nullable() }),
    );
    items.push(...page.items);
    if (!page.nextCursor) break;
    q.set("cursor", page.nextCursor);
  } while (true);
  return items;
}
function selection(accountIds: string[]) {
  const q = new URLSearchParams();
  accountIds.forEach((id) => q.append("accountIds", id));
  return q;
}
