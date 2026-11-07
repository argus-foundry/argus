import { SandboxSettings } from './sandbox/settings-sandbox.js';
import { AgentSettings } from './agents/settings-agents.js';
import { NotificationSettings } from './notifications/settings-notifications.js';
import { GeneralSettings } from './settings-general.js';
import { ProjectSettings } from './projects/settings-projects.js';
import { ScheduleSettings } from './schedules/settings-schedules.js';
import { SystemSettings } from './system/settings-system.js';
import { LogSettings } from './system/settings-logs.js';
import { MaintenanceSettings } from './system/settings-maintenance.js';
import { UpdateSettings } from './system/settings-update.js';

export const settingsSections = [
  { id: 'general', label: 'General', group: 'Personal', keywords: 'defaults knowledge base vault remote API', create: c => new GeneralSettings(c.api, c.host, c.status, c.changed, c.baseline, c.busy) },
  { id: 'notifications', label: 'Notifications & devices', group: 'Personal', keywords: 'push alerts needs input idle browser banners iOS share API tokens', create: c => new NotificationSettings(c) },
  { id: 'agents', label: 'Agents', group: 'Agents & environment', keywords: 'backends accounts', create: c => new AgentSettings(c) },
  { id: 'sandbox', label: 'Sandbox', group: 'Agents & environment', keywords: 'deny read extra write proposals', create: c => new SandboxSettings(c) },
  { id: 'schedules', label: 'Schedules', group: 'Automation', keywords: 'cron recurring one shot', create: context => new ScheduleSettings(context) },
  { id: 'system', label: 'System', group: 'System', keywords: 'metrics host sleep', create: context => new SystemSettings(context) },
  { id: 'maintenance', label: 'Maintenance', group: 'System', keywords: 'cleanup completed tasks worktrees branches', create: context => new MaintenanceSettings(context) },
  { id: 'update', label: 'Update', group: 'System', keywords: 'Argus source path rebuild restart daemon', create: context => new UpdateSettings(context) },
  { id: 'logs', label: 'Logs', group: 'System', keywords: 'UX daemon', create: context => new LogSettings(context) },
];
export function createSettingsSection(id, context) {
  const section = settingsSections.find(section => section.id === id);
  if (section) return section.create(context);
  const target = id.startsWith('repository:') ? id.slice('repository:'.length) : id === 'add-repository' ? null : undefined;
  return new ProjectSettings(context.api, context.host, context.status, context.changed, context.busy, { target });
}
export const validSettingsSection = id => settingsSections.some(section => section.id === id) || id === 'add-repository' || id.startsWith('repository:');

export function settingsSectionTitle(id) {
  return settingsSections.find(section => section.id === id)?.label || (id.startsWith('repository:') ? id.slice('repository:'.length) : 'Add repository');
}
