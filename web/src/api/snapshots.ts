interface Coverage {
  quality: string;
  reasons: string[];
}
export interface Group {
  valueUsd: string | null;
  pricedSubtotalUsd: string | null;
  share: string | null;
  coverage: Coverage;
}
export interface Overview {
  batchId: string;
  asOf: string;
  totalUsd: string | null;
  pricedSubtotalUsd: string | null;
  allocation: (Group & { wallet: string })[];
  assetClassAllocation: (Group & { assetClass: string })[];
  coverage: Coverage;
}
interface Holding {
  accountId: string;
  wallet: string;
  itemKey: string;
  asset: string;
  assetClass: string;
  quantity: string;
  valueUsd: string | null;
  coverage: Coverage;
}
interface Page {
  items: Holding[];
  nextCursor: string | null;
}
export class RequestError extends Error {
  constructor(readonly status: number) {
    super("读取失败");
  }
}
async function get<T>(path: string, signal: AbortSignal): Promise<T> {
  const r = await fetch("/api/v1/" + path, { signal });
  if (!r.ok) throw new RequestError(r.status);
  return r.json();
}
export async function latest(signal: AbortSignal) {
  const overview = await get<Overview>("overview?basis=latest", signal);
  const items: Holding[] = [];
  const q = new URLSearchParams({ batchId: overview.batchId });
  do {
    const page = await get<Page>("holdings?" + q, signal);
    items.push(...page.items);
    if (!page.nextCursor) break;
    q.set("cursor", page.nextCursor);
  } while (true);
  return { overview, items };
}
