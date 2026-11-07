import {roleTokenTotals} from '../composer/composer-context-policy.js';

const array = value => Array.isArray(value) ? value : [];
const measured = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
const dollars = value => new Intl.NumberFormat('en-US',{style:'currency',currency:'USD',minimumFractionDigits:2,maximumFractionDigits:2}).format(value);

export function taskCostRows(task, roster) {
  const matches = array(roster?.orchestrators).flatMap(orch => array(orch.roles).map(role => ({role,orch})))
    .concat(array(roster?.freelance).map(role => ({role})))
    .filter(({role}) => role.live && role.task_id === task.id);
  const coordinators = matches.filter(({role}) => role.kind === 'coordinator');
  const candidates = coordinators.length ? coordinators : matches;
  if (candidates.length !== 1) return [];
  const {role,orch} = candidates[0], rows = [];
  if (measured(role.cost_usd)) rows.push(['Estimated cost',dollars(role.cost_usd)]);
  if (role.kind === 'coordinator' && measured(orch?.subtree_cost_usd)) rows.push(['Team total (this team)',`${dollars(orch.subtree_cost_usd)} (estimate, excludes nested teams)`]);
  if (rows.length) rows.push(...roleTokenTotals(role));
  return rows;
}
