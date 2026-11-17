import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
const result = await build({
  entryPoints: ["src/api/snapshots.ts"],
  bundle: true,
  platform: "node",
  format: "esm",
  write: false,
});
const api = await import(
  "data:text/javascript;base64," +
    Buffer.from(result.outputFiles[0].text).toString("base64")
);
const id = "00000000-0000-4000-8000-000000000001",
  batch = "00000000-0000-4000-8000-000000000010";
const coverage = { quality: "partial", reasons: ["缺价"] };
const overview = {
  batchId: batch,
  asOf: "2026-01-02T02:00:00Z",
  totalUsd: null,
  pricedSubtotalUsd: "800",
  allocation: [],
  assetClassAllocation: [],
  coverage,
};
test("account selection stays on overview and fixed-batch details; unknown amount survives", async () => {
  const requests = [];
  globalThis.fetch = async (url) => {
    requests.push(url);
    return Response.json(
      requests.length === 1 ? overview : { items: [], nextCursor: null },
    );
  };
  const data = await api.latest(new AbortController().signal, [id]);
  assert.equal(data.overview.totalUsd, null);
  assert.equal(data.overview.pricedSubtotalUsd, "800");
  assert.match(requests[0], new RegExp("accountIds=" + id));
  assert.match(requests[1], new RegExp("accountIds=" + id));
  assert.match(requests[1], new RegExp("batchId=" + batch));
});
test("invalid decimal from server is rejected before rendering", async () => {
  globalThis.fetch = async () => Response.json({ ...overview, totalUsd: 800 });
  await assert.rejects(api.latest(new AbortController().signal));
});
test("account mutation sends JSON without persisting credentials in storage", async () => {
  let request;
  globalThis.fetch = async (url, init) => {
    request = { url, init };
    return Response.json({
      id,
      alias: "A",
      exchange: "binance",
      enabledFrom: "2026-01-02T02:00:00Z",
      disabledAt: null,
    });
  };
  await api.bindAccount({
    alias: "A",
    apiKey: "synthetic",
    apiSecret: "synthetic",
  });
  assert.equal(request.init.method, "POST");
  assert.equal(request.url, "/api/v1/accounts");
  assert.deepEqual(JSON.parse(request.init.body), {
    alias: "A",
    apiKey: "synthetic",
    apiSecret: "synthetic",
  });
});
