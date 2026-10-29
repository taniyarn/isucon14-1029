// Example scripted flow. Export default async (page, { persona, cfg, step }).
// Everything the page requests during this function is recorded as "flow:<name>".
export default async function (page, { cfg, step }) {
  await step("open client top");
  await page.goto(new URL("/client", cfg.baseURL).toString(), { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(2000);
  await step("open history via link if present");
  const link = page.locator('a[href*="/client/history"]').first();
  if (await link.count()) {
    await link.click();
    await page.waitForTimeout(2000);
  }
}
