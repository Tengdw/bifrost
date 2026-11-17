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
        path === "overview?basis=latest" &&
        (await r.json()).code === "no_snapshot",
    );
  return schema.parse(await r.json());
}
export async function latest(signal: AbortSignal) {
  const overview = await get("overview?basis=latest", signal, overviewSchema);
  const items: Holding[] = [];
  const q = new URLSearchParams({ batchId: overview.batchId });
  do {
    const page = await get("holdings?" + q, signal, pageSchema);
    items.push(...page.items);
    if (!page.nextCursor) break;
    q.set("cursor", page.nextCursor);
  } while (true);
  return { overview, items };
}
