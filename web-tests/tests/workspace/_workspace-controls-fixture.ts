export async function refreshWorkspace(page) {
  const response = page.waitForResponse(response => new URL(response.url()).pathname === '/api/tasks');
  await page.locator('#connection-banner button').evaluate(button => button.click());
  await response;
  await page.waitForFunction(() => !document.querySelector('#connection-banner button').disabled);
}
export async function archiveSelected(page) {
  await page.locator('#task-title').click();
  await page.keyboard.press('Alt+Shift+A');
}
