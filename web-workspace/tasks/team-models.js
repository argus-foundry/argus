import {coordinatorContext} from '../workspace/split/split-policy.js';

export function teamModelRows(task,roster={}) {
 const team=coordinatorContext(task.id,roster);if(!team)return [];
 const rows=[['Team model details',roster.stale?'Stale. Roster refresh failed.':'Resolved configuration, not provider-confirmed runtime telemetry']];
 const roles=[...(team.roles||[]),...(roster.freelance||[]).filter(role=>role.orch_id===team.id)];
 for(const role of roles){
  const label=role.name||`Role ${role.role_id}`,resolution=role.model_resolution;
  rows.push([`${label} · Archetype`,role.archetype||'Not specified']);
  if(!resolution){rows.push([`${label} · Model`,'Unavailable']);continue;}
  if(resolution.requested_model)rows.push([`${label} · Requested model`,resolution.requested_model]);
  rows.push([`${label} · Effective model`,resolution.source==='unavailable'?'Unavailable':resolution.effective_model||(resolution.source==='cli_default'?'CLI default':'Unavailable')]);
  rows.push([`${label} · Basis`,resolution.basis==='planned_intent'?'Planned':resolution.basis==='current_config'?'Resolved from current settings':'Unavailable']);
  if(resolution.effort)rows.push([`${label} · Effort`,resolution.effort]);
  if(resolution.profile)rows.push([`${label} · Profile`,resolution.profile]);
  if(resolution.warning)rows.push([`${label} · Warning`,resolution.warning]);
 }
 return rows;
}
