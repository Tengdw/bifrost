import { z } from "zod";
import { decimal, get } from "./snapshots";

const instant = z.iso.datetime({ offset: true });
const direction = z.enum(["BOTH", "LONG", "SHORT"]);
const asset = z.object({
  asset: z.string(),
  positionSide: direction.nullish(),
});
const coverage = z.object({
  startedAt: instant.nullable(),
  through: instant.nullable(),
  state: z.enum(["pending", "collecting", "covered", "partial"]),
  reason: z.string().nullish(),
});
const snapshot = z
  .object({ observedAt: instant, quantity: decimal })
  .nullable();
const id = z.string();
const order = z.object({
  id,
  symbol: z.string(),
  side: z.string(),
  positionSide: direction.nullish(),
  status: z.string(),
  type: z.string(),
  quantity: decimal,
  executedQuantity: decimal,
  createdAt: instant,
  updatedAt: instant,
  preexisting: z.boolean(),
});
const fill = z.object({
  id,
  orderId: id,
  symbol: z.string(),
  side: z.string(),
  positionSide: direction.nullish(),
  quantity: decimal,
  quoteQuantity: decimal,
  price: decimal,
  fee: decimal,
  feeAsset: z.string(),
  at: instant,
  delta: decimal,
  balanceAfter: decimal.nullable(),
});
const optionsSchema = z.object({ assets: z.array(asset), coverage });
const detailSchema = optionsSchema.extend({
  opening: snapshot,
  closing: snapshot,
  explainedChange: decimal.nullable(),
  unexplainedDifference: decimal.nullable(),
  orders: z.array(order),
  fills: z.array(fill),
});
export type ReplayOptions = z.infer<typeof optionsSchema>;
export type ReplayDetail = z.infer<typeof detailSchema>;
export type Market = "spot" | "usdm";
export type Direction = z.infer<typeof direction>;

interface ReplayInterval {
  from: string;
  to: string;
}

export interface ReplayQuery extends ReplayInterval {
  accountId: string;
  wallet: Market;
  asset: string;
  positionSide: Direction;
}

export function replayOptions(
  signal: AbortSignal,
  accountId: string,
  wallet: Market,
  interval: ReplayInterval,
) {
  return get(
    "replay?" + new URLSearchParams({ accountId, wallet, ...interval }),
    signal,
    optionsSchema,
  );
}

export function replayDetail(signal: AbortSignal, query: ReplayQuery) {
  const { positionSide, ...fields } = query;
  return get(
    "replay?" +
      new URLSearchParams({
        ...fields,
        ...(fields.wallet === "usdm" ? { positionSide } : {}),
      }),
    signal,
    detailSchema,
  );
}
