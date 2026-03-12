import { defineConfig } from "@playwright/test";

const BASE_URL = "http://127.0.0.1:8077";

export default defineConfig({
  testDir: ".",
  testMatch: "*.test.ts",
  timeout: 30_000,
  use: {
    baseURL: BASE_URL,
  },
  webServer: {
    command: "direnv exec .. go test -run ^$ -v ../internal/web/ -timeout 0",
    env: { SKIPPER_PLAYWRIGHT: "1" },
    url: `${BASE_URL}/healthz`,
    reuseExistingServer: true,
    stdout: "pipe",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  reporter: "list",
});
