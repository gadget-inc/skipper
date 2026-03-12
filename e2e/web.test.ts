import { test, expect } from "@playwright/test";

test("functions page prefills search from URL param", async ({ page }) => {
  await page.goto("/functions?search=web");
  const input = page.locator('input[placeholder="Search functions\u2026"]');
  await expect(input).toHaveValue("web");
  // Only web-app should be visible
  await expect(page.locator("table tbody tr")).toHaveCount(1);
  await expect(page.locator("table tbody")).toContainText("web-app");
});

test("typing in search updates URL via syncParams", async ({ page }) => {
  await page.goto("/functions");
  // syncParams is defined in layout.html and updates the URL search params.
  // Datastar's data-on-keyup calls it after debounce; we invoke it directly
  // to test the function independently of Datastar's event binding.
  await page.evaluate(() => {
    (window as any).syncParams({ search: "api", sort: "deployment", dir: "asc" });
  });
  expect(page.url()).toContain("search=api");
});

test("clicking sort column updates URL via syncParams", async ({ page }) => {
  await page.goto("/functions");
  // syncParams is called by Datastar's data-on-click handler on column headers.
  // We invoke it directly to test the URL update logic.
  await page.evaluate(() => {
    (window as any).syncParams({ search: "", sort: "namespace", dir: "asc" });
  });
  expect(page.url()).toContain("sort=namespace");
  // Empty values should be removed from the URL
  expect(page.url()).not.toContain("search=");
});

test("URL state is preserved across page refresh", async ({ page }) => {
  await page.goto("/functions?search=worker&sort=tenant&dir=desc");
  const input = page.locator('input[placeholder="Search functions\u2026"]');
  await expect(input).toHaveValue("worker");
  await expect(page.locator("table tbody")).toContainText("worker");

  // Refresh
  await page.reload();
  await expect(input).toHaveValue("worker");
  // URL should still have params
  expect(page.url()).toContain("search=worker");
});

test("tenants page search and sort via URL", async ({ page }) => {
  await page.goto("/tenants?search=tenant-1");
  const input = page.locator('input[placeholder="Search tenants\u2026"]');
  await expect(input).toHaveValue("tenant-1");
});

test("events page prefills function filter from URL", async ({ page }) => {
  await page.goto("/events?function=web");
  const input = page.locator('input[placeholder="Filter by function\u2026"]');
  await expect(input).toHaveValue("web");
});

test("events page prefills severity from URL", async ({ page }) => {
  await page.goto("/events?severity=1");
  const select = page.locator("select");
  await expect(select).toHaveValue("1");
});
