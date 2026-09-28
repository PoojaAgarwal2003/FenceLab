import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#status")).toContainText("REPLAY READY");
});

test("paused worker comparison exposes stale effects without changing the schedule", async ({ page }, testInfo) => {
  await expect(page.locator(".policy-card .metric")).toHaveText(["2", "1", "1"]);
  await page.screenshot({ path: testInfo.outputPath("laboratory.png"), fullPage: true });
  await page.getByRole("button", { name: "Inspect Lease only", exact: true }).click();
  await expect(page.locator("#replay-verdict")).toContainText("2 INVARIANT VIOLATIONS");
  await expect(page.locator("#policy-explanation")).toContainText("cannot undo");
  const results = await page.request.get("/api/compare");
  const json = await results.json();
  expect(json[0].plan).toEqual(json[1].plan);
  expect(json[1].plan).toEqual(json[2].plan);
});

test("lost acknowledgments require idempotency in addition to fencing", async ({ page }) => {
  await page.locator("#scenario").selectOption("lost-ack");
  await page.getByRole("button", { name: "Run comparison" }).click();
  await expect(page.locator(".policy-card .metric")).toHaveText(["3", "2", "1"]);
  await page.getByRole("button", { name: "Inspect Fencing tokens", exact: true }).click();
  await expect(page.locator("#replay-verdict")).toContainText("1 INVARIANT VIOLATIONS");
  await page.getByRole("button", { name: "Inspect Fencing + idempotency", exact: true }).click();
  await expect(page.locator("#replay-verdict")).toHaveText("NO VIOLATIONS IN THIS RUN");
  await expect(page.locator("#trace")).toContainText("effect deduplicated");
});

test("replay controls show the state at the selected event", async ({ page }) => {
  await page.locator("#cursor").fill("0");
  await expect(page.locator("#explanation")).toContainText("lease granted");
  await expect(page.locator("#store-state")).toHaveText("fence 0 / writes 0");
  await expect(page.locator("#previous")).toBeDisabled();
  await page.getByRole("button", { name: "Next event" }).click();
  await expect(page.locator("#store-state")).toHaveText("fence 1 / writes 0");
  await page.getByRole("button", { name: "Replay", exact: true }).click();
  await expect(page.getByRole("button", { name: "Pause", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(page.getByRole("button", { name: "Replay", exact: true })).toBeVisible();
});

test("settings apply explicitly and healthy baseline commits exactly once", async ({ page }) => {
  await page.locator("#seed").fill("42");
  await page.locator("#scenario").selectOption("healthy");
  await expect(page.locator("#status")).toContainText("Settings changed");
  await expect(page.locator(".policy-card .metric")).toHaveText(["2", "1", "1"]);
  await page.getByRole("button", { name: "Run comparison" }).click();
  await expect(page.locator("#status")).toContainText("SEED 42");
  await expect(page.locator(".policy-card .metric")).toHaveText(["1", "1", "1"]);
  await expect(page.locator(".policy-card .badge")).toHaveText(["SAFE IN RUN", "SAFE IN RUN", "SAFE IN RUN"]);
});

test("export contains the actual replay and all three policies", async ({ page }) => {
  const downloaded = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export JSON" }).click();
  const download = await downloaded;
  expect(download.suggestedFilename()).toBe("fencelab-paused-worker-seed-7.json");
  const stream = await download.createReadStream();
  const chunks = [];
  for await (const chunk of stream) chunks.push(chunk);
  const exported = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  const actual = await (await page.request.get("/api/compare")).json();
  expect(exported).toEqual(actual);
});

test("failed request displays an error rather than fresh-looking results", async ({ page }) => {
  await page.route("**/api/compare?**", route => route.fulfill({
    status: 429, contentType: "application/json", body: JSON.stringify({ error: "all simulation slots are busy; retry shortly" })
  }));
  await page.getByRole("button", { name: "Run comparison" }).click();
  await expect(page.getByRole("alert")).toContainText("all simulation slots are busy");
  await expect(page.locator("#status")).toContainText("previous run");
  await expect(page.getByRole("button", { name: "Export JSON" })).toBeDisabled();
  await page.unroute("**/api/compare?**");
  await page.getByRole("button", { name: "Run comparison" }).click();
  await expect(page.getByRole("alert")).toBeHidden();
  await expect(page.locator("#status")).toContainText("REPLAY READY");
});

test("bounded input remains readable without page overflow or script errors", async ({ page }) => {
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.locator("#lease").fill("20");
  await page.locator("#seed").fill("-9007199254740991");
  await page.getByRole("button", { name: "Run comparison" }).click();
  await expect(page.locator("#status")).toContainText("SEED -9007199254740991");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(errors).toEqual([]);
  expect(await page.locator("#timeline circle").count()).toBeGreaterThan(10);
});

test("workbench uses a light execution-first layout rather than the old card dashboard", async ({ page }) => {
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Ownership ends.Execution doesn't.");
  expect(await page.locator("body").evaluate(node => getComputedStyle(node).backgroundColor)).toBe("rgb(245, 243, 237)");
  await expect(page.locator('meta[name="color-scheme"]')).toHaveAttribute("content", "light");
  await expect(page.locator(".policy-grid, .controls, .topbar")).toHaveCount(0);
  const setup = await page.locator(".setup").boundingBox();
  const replay = await page.locator(".replay-panel").boundingBox();
  const ledger = await page.locator(".comparison").boundingBox();
  expect(setup.y + setup.height).toBeLessThan(replay.y);
  if (page.viewportSize().width > 850) {
    expect(ledger.x).toBeGreaterThanOrEqual(replay.x + replay.width - 1);
    expect(Math.abs(ledger.y - replay.y)).toBeLessThan(1);
  } else {
    expect(ledger.y).toBeGreaterThanOrEqual(replay.y + replay.height - 1);
  }
  const policies = await page.locator(".policy-card").all();
  for (let i = 1; i < policies.length; i++) {
    const previous = await policies[i - 1].boundingBox();
    const current = await policies[i].boundingBox();
    expect(current.y).toBeGreaterThanOrEqual(previous.y + previous.height - 1);
  }
});

test("keyboard navigation retains focus when selecting policies and journal events", async ({ page }) => {
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "Skip to execution replay" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("#workbench")).toBeFocused();
  const policy = page.getByRole("button", { name: "Inspect Lease only", exact: true });
  await policy.focus();
  await page.keyboard.press("Enter");
  await expect(policy).toBeFocused();
  await expect(policy).toHaveAttribute("aria-pressed", "true");
  const event = page.getByRole("button", { name: "Inspect event 1: lease granted", exact: true });
  await event.focus();
  await page.keyboard.press("Enter");
  await expect(event).toBeFocused();
  await expect(event).toHaveAttribute("aria-current", "step");
  await expect(page.locator("#cursor")).toHaveValue("0");
  await expect(page.locator("#explanation")).toContainText("lease granted");
});

test("narrow phones, tablets and wide screens keep controls and overflow regions usable", async ({ page }) => {
  for (const width of [320, 768, 1024, 1440, 1920]) {
    await page.setViewportSize({ width, height: 1000 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    for (const selector of ["#scenario", "#seed", "#lease", "#run", "#play", "#download"]) {
      const box = await page.locator(selector).boundingBox();
      expect(box.width).toBeGreaterThan(30);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
    }
    const region = page.getByRole("region", { name: "Scrollable execution timeline" });
    await region.evaluate(node => { node.scrollLeft = node.scrollWidth; });
    expect(await region.evaluate(node => node.scrollLeft + node.clientWidth >= node.scrollWidth - 1)).toBe(true);
    if (width === 320) {
      const journal = page.getByRole("region", { name: "Scrollable event journal" });
      await journal.evaluate(node => { node.scrollLeft = node.scrollWidth; });
      expect(await journal.evaluate(node => node.scrollLeft)).toBeGreaterThan(0);
    }
  }
});
