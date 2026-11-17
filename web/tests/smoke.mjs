// 仅用于 synthetic 模式。运行前创建临时会话文件；脚本不读取项目 .key。
import { chromium } from "playwright-core";
import { readFile } from "node:fs/promises";
import assert from "node:assert/strict";
const base = process.env.BIFROST_SMOKE_URL;
if (!base || !process.env.BIFROST_SMOKE_SESSION_FILE)
  throw Error("Set BIFROST_SMOKE_URL and BIFROST_SMOKE_SESSION_FILE");
const token = (
  await readFile(process.env.BIFROST_SMOKE_SESSION_FILE, "utf8")
).trim();
const browser = await chromium.launch({
  headless: true,
  executablePath:
    process.env.CHROME_BIN ||
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
});
const page = await browser.newPage();
try {
  await page.context().addCookies([
    {
      name: "bifrost_session",
      value: token,
      url: base,
      httpOnly: true,
      sameSite: "Strict",
    },
  ]);
  const response = await page.request.get(base + "/api/v1/capabilities");
  assert.equal(
    (await response.json()).mode,
    "synthetic",
    "Smoke must run against synthetic workspace",
  );
  await page.goto(base + "/latest");
  await page.getByText("账号管理", { exact: true }).waitFor();
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
  const select = page.getByRole("combobox", { name: "账号范围", exact: true });
  for (const alias of aliases) {
    await select.click();
    await page.getByText(alias, { exact: true }).last().click();
    await page.keyboard.press("Escape");
  }
  await page.getByRole("button", { name: "手动刷新", exact: true }).click();
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
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  console.log(
    "Synthetic UI smoke passed: two account binding, all-wallet refresh, 9,044.4 combined, 8,794.4 single, classification and mobile.",
  );
} finally {
  await browser.close();
}
