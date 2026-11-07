export async function confirmClick(page, trigger, handle) {
  await trigger();
  const dialog = page.locator('#confirm-dialog');
  await dialog.waitFor();
  const message = await dialog.innerText();
  let pending;
  handle({
    message: () => message,
    accept: () => pending = dialog.getByRole('button', {
      name: 'Confirm',
      exact: true
    }).click(),
    dismiss: () => pending = dialog.getByRole('button', {
      name: 'Cancel',
      exact: true
    }).click()
  });
  await pending;
  await dialog.waitFor({
    state: 'detached'
  });
}
