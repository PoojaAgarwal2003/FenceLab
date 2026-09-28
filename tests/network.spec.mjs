import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#network-status")).toContainText("V2 scenarios ready");
});

test("delayed fence exposes eager-dispatch failure with an inspectable network trace", async ({ page }, testInfo) => {
  await page.locator("#network-example").selectOption("eager");
  await page.getByRole("button", { name: "Run transport", exact: true }).click();
  await expect(page.locator("#network-status")).toHaveText("V2 RUN READY");
  await expect(page.locator("#search-report")).toContainText("INVARIANT VIOLATION");
  await expect(page.locator("#search-report")).toContainText("1 stale");
  await expect(page.locator("#network-trace")).toContainText("message delivered");
  await expect(page.locator("#network-trace")).toContainText("fault delay");
  await page.locator("#network-cursor").fill("0");
  await expect(page.locator("#network-explanation")).toContainText("epoch issued");
  await page.getByRole("button", { name: "Inspect transport event 2", exact: true }).click();
  await expect(page.getByRole("button", { name: "Inspect transport event 2", exact: true })).toHaveAttribute("aria-current", "step");
  await page.locator("#network-trace .trace-event").filter({ hasText: "effect committed" }).first().click();
  await expect(page.locator("#network-explanation")).toContainText("Attempt token 1; active epoch 2; storage fence 1");
  await page.locator(".network-lab").screenshot({ path: testInfo.outputPath("transport-laboratory.png") });
});

test("barrier exploration exhausts a fixed scenario without claiming a universal proof", async ({ page }) => {
  await page.getByRole("button", { name: "Find counterexample", exact: true }).click();
  await expect(page.locator("#network-status")).toHaveText("V2 SEARCH READY");
  await expect(page.locator("#search-report")).toContainText("exhausted");
  await expect(page.locator("#search-report")).toContainText("0 incomplete jobs");
  await expect(page.locator("#search-report")).toContainText("Not arbitrary delays");
  await expect(page.getByRole("button", { name: "Export replay", exact: true })).toBeDisabled();
  await expect(page.locator("#network-trace")).toBeEmpty();
});

test("shortest counterexample exports and imports an exact decision replay", async ({ page }) => {
  await page.locator("#network-example").selectOption("eager");
  const responsePromise = page.waitForResponse(response => response.url().endsWith("/api/v2/search"));
  await page.getByRole("button", { name: "Find counterexample", exact: true }).click();
  const report = await (await responsePromise).json();
  await expect(page.locator("#search-report")).toContainText("counterexample");
  await expect(page.locator("#search-report")).toContainText("Shortest violating");
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export replay", exact: true }).click();
  const download = await downloadPromise;
  const stream = await download.createReadStream();
  const chunks = [];
  for await (const chunk of stream) chunks.push(chunk);
  const bytes = Buffer.concat(chunks);
  expect(JSON.parse(bytes.toString())).toEqual(report.witness);
  const replayResponse = page.waitForResponse(response => response.url().endsWith("/api/v2/replay"));
  await page.locator("#replay-file").setInputFiles({ name: "replay.json", mimeType: "application/json", buffer: bytes });
  expect(await (await replayResponse).json()).toEqual(report.counterexample);
  await expect(page.locator("#network-status")).toHaveText("V2 REPLAY READY");
  await expect(page.locator("#search-report")).toContainText("prefix");
  await expect(page.locator("#network-example")).toHaveValue("custom");
});

test("state and depth cutoffs remain explicitly inconclusive", async ({ page }) => {
  await page.locator("#max-depth").fill("1");
  await page.getByRole("button", { name: "Find counterexample", exact: true }).click();
  await expect(page.locator("#search-report")).toContainText("depth-limit");
  await expect(page.locator("#network-explanation")).toContainText("inconclusive");
  await page.locator("#max-depth").fill("80");
  await page.locator("#max-states").fill("1");
  await page.getByRole("button", { name: "Find counterexample", exact: true }).click();
  await expect(page.locator("#search-report")).toContainText("state-limit");
  await expect(page.getByRole("button", { name: "Export replay", exact: true })).toBeDisabled();
});

test("strict scenario errors keep previous results labeled stale and disable export", async ({ page }) => {
  await page.getByRole("button", { name: "Run transport", exact: true }).click();
  await expect(page.locator("#network-status")).toHaveText("V2 RUN READY");
  const original = await page.locator("#network-json").inputValue();
  await page.locator("#network-json").fill(original.replace('"version":', '"version":"fencelab/v2","version":'));
  await page.getByRole("button", { name: "Run transport", exact: true }).click();
  await expect(page.locator("#network-error")).toContainText("duplicate");
  await expect(page.locator("#network-status")).toContainText("previous run");
  await expect(page.getByRole("button", { name: "Export replay", exact: true })).toBeDisabled();
  await page.locator("#network-json").fill(original);
  await page.getByRole("button", { name: "Run transport", exact: true }).click();
  await expect(page.locator("#network-error")).toBeHidden();
  await expect(page.locator("#network-status")).toHaveText("V2 RUN READY");
});

test("partition healing and lost-result duplicates remain visible in the journal", async ({ page }) => {
  await page.locator("#network-example").selectOption("partition");
  await page.getByRole("button", { name: "Run transport", exact: true }).click();
  await expect(page.locator("#network-status")).toHaveText("V2 RUN READY");
  await expect(page.locator("#network-trace")).toContainText("partition held");
  await expect(page.locator("#network-trace")).toContainText("heal");
  await expect(page.locator("#search-report")).toContainText("No violations observed");
  await page.locator("#network-example").selectOption("lost-result");
  await page.getByRole("button", { name: "Run transport", exact: true }).click();
  await expect(page.locator("#network-trace")).toContainText("fault drop");
  await expect(page.locator("#network-trace")).toContainText("fault duplicate");
  await expect(page.locator("#network-trace")).toContainText("effect deduplicated");
  await expect(page.locator("#search-report")).toContainText("1 effects");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
