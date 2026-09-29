import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  outputDir: "./web-test-results",
  fullyParallel: true,
  // All tests share one server whose model/artifact executor has one slot.
  // Concurrent success-path tests would correctly receive 429 from each other.
  workers: 1,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:18091",
    trace: "retain-on-failure"
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 1100 } } },
    { name: "mobile", use: { ...devices["Pixel 7"] } }
  ],
  webServer: {
    command: "go run ./cmd/fencelab serve -listen 127.0.0.1:18091",
    url: "http://127.0.0.1:18091/api/health",
    reuseExistingServer: false,
    timeout: 60000
  }
});
