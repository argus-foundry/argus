export function generalPayload(values) {
  return {
    kb: { enabled: !!values.kbEnabled, metis_vault_path: values.vaultPath.trim() },
    defaults: { backend: values.backend, share_project: values.shareProject },
    api: { enabled: !!values.apiEnabled },
  };
}
const lines = value => value.split('\n').map(line => line.trim()).filter(Boolean);
export function projectPayload(values, original = {}) {
  const result = { ...structuredClone(original), name: values.name.trim(), path: values.path.trim(), branch: values.branch.trim(), backend: values.backend };
  if (!result.name || !result.path) throw new Error('Project name and repository path are required.');
  const sandbox = { ...result.sandbox, enabled: values.sandbox === 'inherit' ? null : values.sandbox === 'true', deny_read: lines(values.denyRead), extra_write: lines(values.extraWrite) };
  if (result.sandbox || sandbox.enabled !== null || sandbox.deny_read.length || sandbox.extra_write.length) result.sandbox = sandbox;
  if (new TextEncoder().encode(JSON.stringify(result)).length > 4096) throw new Error('Project configuration exceeds the existing 4 KiB limit. Shorten the fields before saving.');
  return result;
}
export function settingsURL(url, section) {
  const next = new URL(url);
  if (section) next.searchParams.set('settings', section); else next.searchParams.delete('settings');
  return next;
}
