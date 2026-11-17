// 仅用于使用独立临时数据库的 synthetic 模式。
import { chromium } from "playwright-core";
import { readFile } from "node:fs/promises";
import assert from "node:assert/strict";
const base = process.env.BIFROST_SMOKE_URL;
if (!base || !process.env.BIFROST_SMOKE_PASSWORD_FILE)
  throw Error("Set BIFROST_SMOKE_URL and BIFROST_SMOKE_PASSWORD_FILE");
const password = (
  await readFile(process.env.BIFROST_SMOKE_PASSWORD_FILE, "utf8")
).trim();
const browser = await chromium.launch({
  headless: true,
  executablePath:
    process.env.CHROME_BIN ||
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
});
const page = await browser.newPage();
try {
  await page.goto(base + "/accounts");
  await page.getByRole("heading", { name: "登录工作区" }).waitFor();
  const passwordOption = page.getByRole("button", { name: "使用账号密码" });
  if (await passwordOption.count()) await passwordOption.click();
  await page
    .getByLabel("用户名", { exact: true })
    .fill(process.env.BIFROST_SMOKE_USERNAME ?? "demo");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("button", { name: "绑定只读账号" }).waitFor();
  const response = await page.request.get(base + "/api/v1/capabilities");
  assert.equal(
    (await response.json()).mode,
    "synthetic",
    "Smoke must run against synthetic workspace",
  );
  const suffix = Date.now().toString();
  const listed = await (
    await page.request.get(base + "/api/v1/accounts")
  ).json();
  const aliases = ["验收A-" + suffix, "验收B-" + suffix];
  for (let i = 0; i < 2; i++) {
    const existing = listed.find(
      (a) => a.alias.startsWith(i ? "验收B-" : "验收A-") && !a.disabledAt,
    );
    if (existing) aliases[i] = existing.alias;
  }
  for (let index = 0; index < 2; index++) {
    if (listed.some((a) => a.alias === aliases[index])) continue;
    await page.getByRole("button", { name: "绑定只读账号" }).click();
    await page.locator("input[name=alias]").fill(aliases[index]);
    await page
      .locator("input[name=apiKey]")
      .fill(
        process.env[index ? "BIFROST_SMOKE_KEY_B" : "BIFROST_SMOKE_KEY_A"] ||
          (index ? "demo-b" : "demo-a"),
      );
    await page.locator("input[name=apiSecret]").fill("fixture-only");
    await page.getByRole("button", { name: "验证并保存" }).click();
    await page.getByLabel("别名 " + aliases[index], { exact: true }).waitFor();
    await page.locator("input[name=apiSecret]").waitFor({ state: "detached" });
  }
  await page.getByRole("link", { name: "最新采集", exact: true }).click();
  const select = page.getByRole("combobox", { name: "账号范围", exact: true });
  for (const alias of aliases) {
    await select.click();
    await page.getByText(alias, { exact: true }).last().click();
    await page.keyboard.press("Escape");
  }
  await page.getByRole("button", { name: "立即采集", exact: true }).click();
  await page.waitForFunction(
    () =>
      Array.from(document.querySelectorAll("td")).filter(
        (e) => e.textContent === "running",
      ).length === 0,
  );
  await page
    .getByText("9,044.4", { exact: true })
    .first()
    .waitFor({ timeout: 30000 });
  const closeA = page
    .locator(".ant-select-selection-item")
    .filter({ hasText: aliases[1] })
    .locator(".ant-select-selection-item-remove");
  await closeA.click();
  await page
    .getByText("8,794.4", { exact: true })
    .first()
    .waitFor({ timeout: 30000 });
  await page.getByText("按资产类别", { exact: true }).click();
  assert.equal(
    await page.locator(".metrics strong").first().textContent(),
    "8,794.4",
  );
  await page.setViewportSize({ width: 375, height: 812 });
  let runningOnce = true;
  let expectedTotal;
  await page.route("**/api/v1/overview?*", async (route) => {
    if (runningOnce) {
      runningOnce = false;
      await route.fulfill({ status: 409, json: { code: "batch_running" } });
      return;
    }
    const response = await route.fetch();
    const data = await response.json();
    expectedTotal = data.totalUsd;
    data.staleWallets = [
      {
        accountId: "00000000-0000-4000-8000-000000000001",
        wallet: "funding",
        batchId: data.batchId,
        observedAt: "2026-01-01T02:00:00Z",
        valueUsd: "1000",
        reason: "合成采集失败",
        items: [
          {
            asset: "EQ_MU",
            assetClass: "us_equity",
            quantity: "2",
            valueUsd: "200",
            itemKey: "EQ_MU",
          },
        ],
        positions: [],
      },
    ];
    await route.fulfill({ json: data });
  });
  await page.reload();
  await page.getByText("正在采集，结束后自动读取…", { exact: true }).waitFor();
  await page.getByText("上次观察（仅供参考）", { exact: true }).waitFor();
  const displayed = await page.locator(".metrics strong").first().textContent();
  assert.equal(displayed.replaceAll(",", ""), expectedTotal);
  await page.getByText("2026-01-01T02:00:00Z", { exact: true }).waitFor();
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await page.getByRole("link", { name: "资产分析", exact: true }).click();
  await page.getByText("暂无日终数据", { exact: true }).waitFor();
  assert.equal(await page.locator(".worth").textContent(), "—");
  await page.getByRole("button", { name: "每日收益率", exact: true }).click();
  await page.getByText("暂无可靠收益数据", { exact: true }).waitFor();
  await page
    .getByRole("button", { name: "查看账号分布", exact: false })
    .click();
  const distributions = page.getByRole("dialog", {
    name: "账号资产分布 · USD",
  });
  await distributions.getByRole("button", { name: "Close" }).click();
  await distributions.waitFor({ state: "hidden" });
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    "Dashboard must fit a mobile viewport",
  );
  console.log(
    "Synthetic UI smoke passed: two account binding, all-wallet refresh, 9,044.4 combined, 8,794.4 single, classification and mobile.",
  );

  const authenticator = await page.context().newCDPSession(page);
  await authenticator.send("WebAuthn.enable");
  await authenticator.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      transport: "internal",
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  });
  await page.getByRole("link", { name: "登录安全" }).click();
  await page.getByText("尚未绑定 Passkey。").waitFor();
  const registered = page.waitForResponse(
    (result) =>
      result.url().endsWith("/auth/passkeys") &&
      result.request().method() === "POST",
  );
  await page.getByRole("button", { name: "添加 Passkey" }).click();
  assert.equal((await registered).status(), 204);
  await page.getByRole("button", { name: "移除", exact: true }).waitFor();
  await page.getByRole("button", { name: "退出登录" }).click();

  const passkey = page.getByRole("button", { name: "使用 Passkey 登录" });
  const signedIn = page.waitForResponse((result) =>
    result.url().endsWith("/auth/login/passkey"),
  );
  await passkey.click();
  assert.equal((await signedIn).status(), 204);
  await page.getByText(/最近使用/).waitFor();
  await page.reload();
  await page.getByText(/最近使用/).waitFor();

  await page.getByRole("button", { name: "移除", exact: true }).click();
  await page.getByRole("button", { name: "确认移除" }).click();
  await page.getByText("尚未绑定 Passkey。").waitFor();
  await page.getByRole("button", { name: "退出登录" }).click();
  const rejected = page.waitForResponse((result) =>
    result.url().endsWith("/auth/login/passkey"),
  );
  await passkey.click();
  assert.equal((await rejected).status(), 401);
  console.log(
    "Passkey registration, login, reload, removal and logout passed.",
  );
} finally {
  await browser.close();
}
