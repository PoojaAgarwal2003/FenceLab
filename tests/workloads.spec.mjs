import { test, expect } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";

let report;
test.beforeAll(() => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "fencelab-browser-workload-"));
  try {
    const file = path.join(temporary, "config.json");
    fs.writeFileSync(file, JSON.stringify({
      version: "fencelab/workload-v1", jobs: 4, capacity: 4, policy: "fair",
      max_attempts: 2, offer_every_ms: 0, service_ms: 1,
      fault_every: 4, fault: "lost-ack-always", reopen_after: 4
    }));
    report = JSON.parse(execFileSync("go", ["run", "./cmd/fencelab", "workload",
      "-file", file, "-dir", path.join(temporary, "run")], { encoding: "utf8", timeout: 60000 }));
  } finally {
    fs.rmSync(temporary, { recursive: true, force: true });
  }
});

const importReport = (page, value) => page.locator("#artifact-file").setInputFiles({
  name: "workload.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(value))
});

test("inspect real workload exhaustion, fairness and elapsed measurements", async ({ page }, testInfo) => {
  await page.goto("/");
  await importReport(page, report);
  await expect(page.locator("#artifact-status")).toHaveText("ARTIFACT CONSISTENCY CHECKED");
  await expect(page.locator("#artifact-summary")).toContainText("3 acknowledged / 1 exhausted");
  await expect(page.locator("#artifact-summary")).toContainText("4 durable effects; 1 deduplicated");
  await expect(page.locator("#workload-capacity")).toHaveText("4 / 4");
  await expect(page.locator("#workload-outcomes")).toHaveAccessibleName("3 acknowledged, 1 exhausted, 0 rejected");
  await expect(page.locator("#workload-dispatches span")).toHaveCount(5);
  await expect(page.locator("#workload-fairness")).toContainText("not a wall-clock SLA");
  await expect(page.locator("#artifact-recovery")).toContainText("no process was killed");
  await expect(page.locator("#artifact-rows tr")).toHaveCount(4);
  await expect(page.locator("#artifact-rows")).toContainText("exhausted");
  await expect(page.locator("#workload-elapsed")).toHaveText(`${(report.elapsed_ns / 1e6).toFixed(2)} ms`);
  await page.locator("#workload-metrics").scrollIntoViewIfNeeded();
  await page.locator(".artifact-lab").screenshot({ path: testInfo.outputPath("workload-laboratory.png") });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("reject changed workload accounting, then clear workload visuals on process import", async ({ page }) => {
  await page.goto("/");
  await importReport(page, report);
  await expect(page.locator("#workload-metrics")).toBeVisible();
  const tampered = structuredClone(report);
  tampered.counts.effects = 99;
  await importReport(page, tampered);
  await expect(page.locator("#artifact-error")).toContainText("metrics disagree");
  await expect(page.locator("#artifact-results")).toBeHidden();
  await page.locator("#artifact-file").setInputFiles("docs/evidence/process-barrier.json");
  await expect(page.locator("#artifact-status")).toHaveText("ARTIFACT CONSISTENCY CHECKED");
  await expect(page.locator("#workload-metrics")).toBeHidden();
  await expect(page.locator("#artifact-summary")).toContainText("No violations observed");
});

test("reject a changed selection journal and allow the same file to be retried", async ({ page }) => {
  await page.goto("/");
  const tampered = structuredClone(report);
  tampered.order[4] = 4;
  await importReport(page, tampered);
  await expect(page.locator("#artifact-error")).toContainText("journal");
  await importReport(page, report);
  await expect(page.locator("#artifact-error")).toBeHidden();
  await expect(page.locator("#artifact-status")).toHaveText("ARTIFACT CONSISTENCY CHECKED");
});

test("inspect curated overload and ambiguous completion reports", async ({ page }, testInfo) => {
  await page.goto("/");
  await page.locator("#artifact-file").setInputFiles("docs/evidence/workload-overload.json");
  await expect(page.locator("#artifact-summary")).toContainText("16 admitted / 112 rejected / 16 acknowledged");
  await expect(page.locator("#artifact-rows tr")).toHaveCount(128);
  await expect(page.locator("#workload-capacity")).toHaveText("16 / 16");
  await expect(page.locator("#workload-outcomes")).toHaveAccessibleName("16 acknowledged, 0 exhausted, 112 rejected");
  await page.locator("#workload-metrics").scrollIntoViewIfNeeded();
  await page.locator(".artifact-lab").screenshot({ path: testInfo.outputPath("workload-overload.png") });
  await page.locator("#artifact-file").setInputFiles("docs/evidence/workload-retry-storm.json");
  await expect(page.locator("#artifact-summary")).toContainText("48 acknowledged / 16 exhausted. 64 durable effects; 32 deduplicated");
  await expect(page.locator("#workload-dispatches span")).toHaveCount(96);
  await expect(page.locator("#artifact-rows tr")).toHaveCount(64);
  await page.locator("#workload-metrics").scrollIntoViewIfNeeded();
  await page.locator(".artifact-lab").screenshot({ path: testInfo.outputPath("workload-retry-storm.png") });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("distinguish fair dispatch bounds from strict-priority starvation", async ({ page }) => {
  await page.goto("/");
  await page.locator("#artifact-file").setInputFiles("docs/evidence/workload-balanced.json");
  await expect(page.locator("#workload-fairness")).toContainText("Weighted fair 3:1");
  await expect(page.locator("#artifact-rows tr").nth(3).locator("td").nth(3)).toHaveText("3");
  await page.locator("#artifact-file").setInputFiles("docs/evidence/workload-priority.json");
  await expect(page.locator("#workload-fairness")).toContainText("No finite starvation bound");
  await expect(page.locator("#artifact-rows tr").nth(3).locator("td").nth(3)).toHaveText("48");
});
