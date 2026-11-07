import { refreshWorkspace, archiveSelected } from '../../_workspace-controls-fixture.ts';
// Uses only the isolated harness.
import assert from 'node:assert/strict';
export async function railAppearance(page, task) {
  const row = page.locator(`.task-row[data-task-id="${task.id}"]`);
  const action = page.getByRole('button', {
    name: `Archive ${task.name}`,
    exact: true
  });
  await page.locator('#task-title').hover();
  assert.equal(await action.evaluate(button => getComputedStyle(button).opacity), '0');
  const background = await row.evaluate(button => getComputedStyle(button).backgroundColor);
  await row.hover();
  assert.equal(await action.isVisible(), true);
  assert.equal(await action.evaluate(button => getComputedStyle(button).opacity), '1');
  const rowBox = await row.boundingBox(),
    actionBox = await action.boundingBox();
  assert.ok(Math.abs(actionBox.y + actionBox.height / 2 - rowBox.y - rowBox.height / 2) <= 1, 'Archive icon must be vertically centered in its row');
  assert.ok(actionBox.x > rowBox.x + rowBox.width * 0.7, 'Archive icon must stay right-aligned');
  await page.waitForFunction(({
    id,
    background
  }) => getComputedStyle(document.querySelector(`.task-row[data-task-id="${id}"]`)).backgroundColor !== background, {
    id: task.id,
    background
  });
  assert.ok(await row.evaluate(button => parseFloat(getComputedStyle(button).borderRadius) > 0));
  assert.equal(await action.locator('svg').evaluate(icon => getComputedStyle(icon).fill), 'none');
}
export async function railArchive(page, initial, second, choose, api, record) {
  const row = task => page.locator(`.task-row[data-task-id="${task.id}"]`);
  const action = task => page.getByRole('button', {
    name: `Archive ${task.name}`,
    exact: true
  });
  await choose(initial.name);
  assert.equal(await page.locator('.task-archive').count(), await page.locator('.task-row').count());
  await railAppearance(page, second);
  await row(second).focus();
  await page.keyboard.press('Tab');
  assert.equal(await action(second).evaluate(button => button === document.activeElement), true);
  assert.equal(await action(second).evaluate(button => getComputedStyle(button).opacity), '1');
  await refreshWorkspace(page);
  assert.equal(await action(second).evaluate(button => button === document.activeElement), true);
  await action(second).click();
  assert.equal(await page.locator('#archive-dialog-title').textContent(), `You're about to archive ${second.name}.`);
  assert.equal(await page.locator('#task-title').textContent(), initial.name);
  assert.match(await page.locator('#archive-dialog-message').textContent(), /intertask messages/);
  await page.getByRole('button', {
    name: 'Cancel',
    exact: true
  }).click();
  assert.equal(Boolean((await api(`/api/tasks/${second.id}`)).archived), false);
  await action(second).click();
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#archive-dialog').count(), 0);
  assert.equal(Boolean((await api(`/api/tasks/${second.id}`)).archived), false);
  record('rail icon is keyboard reachable; opening, Cancel and Escape preserve selection and task');
  await page.locator('#task-groups').evaluate((groups, task) => {
    groups.dispatchEvent(new CustomEvent('task-archive', {
      bubbles: true,
      detail: {
        ...task,
        name: '<img src=x onerror=alert(1)>'
      }
    }));
  }, second);
  assert.equal(await page.locator('#archive-dialog-title').textContent(), "You're about to archive <img src=x onerror=alert(1)>.");
  assert.equal(await page.locator('#archive-dialog-title').locator('*').count(), 0);
  await page.getByRole('button', {
    name: 'Cancel',
    exact: true
  }).click();
  await page.route(`**/api/tasks/${second.id}/archive`, route => route.fulfill({
    status: 503,
    json: {
      error: 'Temporarily unavailable'
    }
  }), {
    times: 1
  });
  await action(second).click();
  await page.locator('#archive-dialog .primary').click();
  await page.locator('#archive-dialog .error-text:not([hidden])').waitFor();
  assert.equal(Boolean((await api(`/api/tasks/${second.id}`)).archived), false);
  assert.equal(await page.locator('#archive-dialog').evaluate(dialog => dialog.open), true);
  await page.locator('#archive-dialog .primary').click();
  await row(second).waitFor({
    state: 'detached'
  });
  assert.equal((await api(`/api/tasks/${second.id}`)).archived, true);
  assert.equal(await page.locator('#task-title').textContent(), initial.name);
  await page.locator('#archive-filter').click();
  const restore = page.getByRole('button', {
    name: `Restore ${second.name}`,
    exact: true
  });
  await row(second).hover();
  await restore.click();
  assert.equal(await page.locator('#archive-dialog-title').textContent(), `You're about to restore ${second.name}.`);
  assert.match(await page.locator('#archive-dialog-message').textContent(), /does not restart/);
  await page.getByRole('button', {
    name: 'Cancel',
    exact: true
  }).click();
  assert.equal((await api(`/api/tasks/${second.id}`)).archived, true);
  await restore.click();
  await page.locator('#archive-dialog .primary').click();
  await row(second).waitFor({
    state: 'detached'
  });
  assert.equal(Boolean((await api(`/api/tasks/${second.id}`)).archived), false);
  assert.equal(await page.locator('#task-title').textContent(), initial.name);
  record('rail archive and restore use existing confirmed operations without selecting the target');
  await page.locator('#active-filter').click();
  await choose(second.name);
  await action(second).click();
  await page.locator('#archive-dialog .primary').click();
  await page.locator('#archive-dialog').waitFor({
    state: 'detached'
  });
  assert.equal(await page.locator('#task-title').textContent(), second.name);
  assert.equal(await page.locator('#active-filter').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('.task-row[aria-current="page"]').count(), 0);
  assert.equal(await page.locator('.xterm').count(), 1);
  assert.equal((await api(`/api/tasks/${second.id}`)).archived, true);
  await choose(initial.name);
  await page.waitForFunction(() => document.querySelector('#terminal-state').textContent === 'Live terminal');
  await page.evaluate(() => {
    window.archiveTerminal = document.querySelector('.xterm');
  });
  await page.route('**/api/tasks?archived=all', route => route.fulfill({
    status: 503,
    json: {}
  }), {
    times: 1
  });
  await archiveSelected(page);
  assert.equal(await page.locator('#archive-dialog-title').textContent(), `You're about to archive ${initial.name}.`);
  await page.locator('#archive-dialog .primary').click();
  await page.locator('#archive-dialog').waitFor({
    state: 'detached'
  });
  assert.equal(await page.locator('#task-title').textContent(), initial.name);
  assert.equal(await page.locator('.task-row').count(), 0);
  assert.equal(await page.locator('.task-row[aria-current="page"]').count(), 0);
  assert.equal(await page.locator('#active-filter').getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('#send').isDisabled(), true);
  assert.equal(await page.evaluate(() => window.archiveTerminal === document.querySelector('.xterm')), true);
  assert.equal((await api(`/api/tasks/${initial.id}`)).archived, true);
  await archiveSelected(page);
  await page.locator('#archive-dialog .primary').click();
  await row(initial).waitFor();
  assert.equal(await page.locator('.task-row[aria-current="page"]').getAttribute('data-task-id'), initial.id);
  record('archiving selection retains its main terminal and rail filter through an empty list and failed refresh; shortcut restore reselects it');
  await page.locator('#archive-filter').click();
  for (const task of [second]) {
    await row(task).hover();
    await page.getByRole('button', {
      name: `Restore ${task.name}`,
      exact: true
    }).click();
    await page.locator('#archive-dialog .primary').click();
    await row(task).waitFor({
      state: 'detached'
    });
  }
  await page.locator('#active-filter').click();
  await choose(initial.name);
}
