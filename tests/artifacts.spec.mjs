import { test, expect } from "@playwright/test";
import fs from "node:fs";

const artifact = name => `docs/evidence/${name}.json`;

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#artifact-status")).toHaveText("No artifact loaded.");
});

test("inspect the real process counterexample and recovered epochs", async ({ page }, testInfo) => {
  await page.locator("#artifact-file").setInputFiles(artifact("process-eager"));
  await expect(page.locator("#artifact-status")).toHaveText("ARTIFACT CONSISTENCY CHECKED");
  await expect(page.locator("#artifact-summary")).toContainText("INVARIANT VIOLATION");
  await expect(page.locator("#artifact-summary")).toContainText("1 stale");
  await expect(page.locator("#artifact-recovery")).toContainText("9 virtual event decisions");
  await expect(page.locator("#artifact-recovery")).toContainText("next reserved token 3");
  await expect(page.locator("#artifact-rows .artifact-commit")).toHaveCount(1);
  await page.locator("#artifact-rows .artifact-commit").scrollIntoViewIfNeeded();
  await page.locator(".artifact-lab").screenshot({ path: testInfo.outputPath("process-laboratory.png") });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("inspect all fifteen actual process crash cases", async ({ page }, testInfo) => {
  await page.locator("#artifact-file").setInputFiles(artifact("durability"));
  await expect(page.locator("#artifact-status")).toHaveText("ARTIFACT CONSISTENCY CHECKED");
  await expect(page.locator("#artifact-rows tr")).toHaveCount(15);
  await expect(page.locator("#artifact-summary")).toContainText("one logical effect");
  await expect(page.locator("#artifact-rows")).toContainText("deduplicated");
  await expect(page.locator("#artifact-recovery")).toContainText("not a power-loss guarantee");
  await page.locator("#artifact-rows tr").last().scrollIntoViewIfNeeded();
  await page.locator(".artifact-lab").screenshot({ path: testInfo.outputPath("recovery-laboratory.png") });
});

test("reject tampered and oversized artifacts without leaving stale success", async ({ page }) => {
  await page.locator("#artifact-file").setInputFiles(artifact("process-barrier"));
  await expect(page.locator("#artifact-summary")).toContainText("No violations observed");
  const report = JSON.parse(fs.readFileSync(artifact("process-barrier"), "utf8"));
  report.history[0].response.token = 99;
  await page.locator("#artifact-file").setInputFiles({ name: "tampered.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(report)) });
  await expect(page.locator("#artifact-error")).toContainText("differs from model");
  await expect(page.locator("#artifact-results")).toBeHidden();
  await expect(page.locator("#artifact-status")).toContainText("Artifact rejected");
  await page.locator("#artifact-file").setInputFiles({ name: "oversized.json", mimeType: "application/json", buffer: Buffer.alloc(65537, 32) });
  await expect(page.locator("#artifact-error")).toContainText("64 KiB");
  await page.locator("#artifact-file").setInputFiles(artifact("process-lost-result"));
  await expect(page.locator("#artifact-error")).toBeHidden();
  await expect(page.locator("#artifact-summary")).toContainText("No violations observed");
  await expect(page.locator("#artifact-recovery")).toContainText("Job acknowledged.");
});

test("show artifact service errors and recover on retry", async ({ page }) => {
  await page.route("**/api/artifacts/check", route => route.fulfill({
    status: 429, contentType: "application/json", body: JSON.stringify({ error: "laboratory is busy" })
  }));
  await page.locator("#artifact-file").setInputFiles(artifact("durability"));
  await expect(page.locator("#artifact-error")).toContainText("laboratory is busy");
  await expect(page.locator("#artifact-file")).toBeEnabled();
  await page.unroute("**/api/artifacts/check");
  await page.locator("#artifact-file").setInputFiles(artifact("durability"));
  await expect(page.locator("#artifact-status")).toHaveText("ARTIFACT CONSISTENCY CHECKED");
});
