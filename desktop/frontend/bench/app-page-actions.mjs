/** Shared production page navigation used by behavior and memory fixtures. */
export async function chooseAppLayout(page, label, className) {
  await page.locator('button:has(svg.lucide-settings)').last().click();
  await page.locator('.settings-screen').waitFor();
  await page.locator('.settings-screen .set-seg__btn').filter({ hasText: new RegExp(`^${label}$`) }).click();
  await page.locator(`.app.${className}`).waitFor();
  await page.locator('.settings-screen .management-screen__back').click();
  await page.locator('.settings-screen').waitFor({ state: 'detached' });
}
