import { node } from '../../shared/dom.js';
import { field, select, actions, values } from '../settings-fields.js';

export function projectForm(host, project, backends, save, cancel) {
  const form = node('form', 'settings-form'), fields = node('fieldset', 'settings-fields'); form.append(fields);
  fields.append(node('legend', '', project ? `Edit ${project.name}` : 'Add project'));
  const name = field(fields, 'Project name', 'name', project?.name || ''); name.required = true; name.disabled = !!project;
  const path = field(fields, 'Repository path', 'path', project?.path || '', 'text', 'Path on the machine running Argus.'); path.required = true;
  field(fields, 'Default branch', 'branch', project?.branch || '');
  select(fields, 'Project agent', 'backend', [['', 'Inherit default'], ...backends.map(backend => [backend.name, backend.name])], project?.backend || '');
  const advanced = node('details', 'settings-advanced'); advanced.append(node('summary', '', 'Project sandbox overrides')); fields.append(advanced);
  select(advanced, 'Project sandbox', 'sandbox', [['inherit', 'Inherit global'], ['true', 'On'], ['false', 'Off']], project?.sandbox?.enabled == null ? 'inherit' : String(project.sandbox.enabled));
  field(advanced, 'Deny read paths', 'denyRead', (project?.sandbox?.deny_read || []).join('\n'), 'textarea', 'One path per line.');
  field(advanced, 'Extra write paths', 'extraWrite', (project?.sandbox?.extra_write || []).join('\n'), 'textarea', 'One path per line. Existing Apple Events overrides are preserved.');
  if (project) {
    const warning = node('p', 'settings-warning', 'The existing project API resets any diligence profile assignment when saving. Reassign the profile in Argus afterward.');
    warning.id = 'project-profile-warning'; fields.append(warning);
  }
  actions(fields, 'Save project', cancel);
  form.addEventListener('submit', event => { event.preventDefault(); save({ ...values(form), name: project?.name || name.value }, fields); });
  host.replaceChildren(form);
}
