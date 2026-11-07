const array = value => Array.isArray(value) ? value : [];
const numeric = value => typeof value === 'number' && Number.isFinite(value);
const count = value => numeric(value) && value >= 0 ? value : null;
const percent = value => numeric(value) && value >= 0 && value <= 100 ? value : null;
const stamp = value => Number.isFinite(Date.parse(value)) && Date.parse(value) > 0 ? value : null;
export const tokenCount = value => new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);

function measuredContext(bridge, meta) {
  const live = bridge?.live === true ? bridge.context : null;
  if (live) {
    const used = count(live.tokens), limit = count(live.window) > 0 ? live.window : null;
    const fill = percent(live.percent) ?? (used !== null && limit ? percent(100 * used / limit) : null);
    if (used !== null || fill !== null) return {used,limit,percent:fill,recorded:false,at:stamp(bridge.last_measure_at)};
  }
  const entries = array(meta?.entries).filter(item => item.namespace === 'hera');
  const size = entries.find(item => item.key === 'context_size');
  const fill = entries.find(item => item.key === 'context_percent');
  const used = size?.value?.trim() ? count(Number(size.value)) : null;
  const same = fill && (!size || fill.updated_at === size.updated_at);
  const pct = same && fill.value?.trim() ? percent(Number(fill.value)) : null;
  if (used === null && pct === null) return null;
  return {used,limit:null,percent:pct,recorded:true,at:stamp(size?.updated_at || fill?.updated_at)};
}

function accountWindows(account, now) {
  const windows = [];
  for (const [key, reset, label] of [['five_hour_pct','five_hour_resets_at','5h limit'],['weekly_pct','weekly_resets_at','7d limit']]) {
    const used = percent(account?.usage?.[key]), at = stamp(account?.usage?.[reset]);
    if (used !== null && at && Date.parse(at) > now) windows.push({label,left:100-used,reset:at});
  }
  return windows;
}

function bridgeWindows(bridge, now) {
  if (bridge?.live !== true) return [];
  return array(bridge.rate_limits).flatMap(window => {
    const label = {five_hour:'5h limit',seven_day:'7d limit'}[window.kind];
    const used = percent(window.percent_used), reset = stamp(window.resets_at);
    return label && used !== null && reset && Date.parse(reset) > now ? [{label,left:100-used,reset}] : [];
  });
}

function roleTotals(task, roster) {
  const roles = [...array(roster?.orchestrators).flatMap(orch => array(orch.roles)), ...array(roster?.freelance)]
    .filter(role => role.live && role.task_id === task.id);
  if (roles.length !== 1) return [];
  return [['Input','tokens_input'],['Cached input','tokens_cache_read'],['Cache write (1h)','tokens_cache_write_1h'],['Cache write (5m)','tokens_cache_write_5m'],['Output','tokens_output']]
    .filter(([,key]) => count(roles[0][key]) > 0).map(([label,key]) => [label,tokenCount(roles[0][key])]);
}

export function contextSnapshot(task, data = {}, now = Date.now()) {
  const raw = data.raw || {}, backend = raw.backend || task.backend;
  const kind = array(data.backends?.backends).find(item => item.name === backend)?.kind;
  const accountName = raw.account || 'default';
  // Raw metadata must have loaded before an absent account means the default account.
  const account = data.raw ? array(data.accounts).find(item => item.name === accountName) : null;
  const identity = [];
  if (raw.account || account) identity.push(['Account',account?.label || accountName]);
  if (kind === 'claude') {
    if (account?.plan) identity.push(['Plan',account.plan]);
    if (account?.email) identity.push(['Email',account.email]);
    if (account?.org) identity.push(['Organization',account.org]);
  }
  const windows = kind === 'claude' ? accountWindows(account,now) : [];
  if (kind === 'claude') for (const window of bridgeWindows(data.bridge,now)) {
    if (!windows.some(item => item.label === window.label)) windows.push(window);
  }
  return {context:measuredContext(data.bridge,data.meta),kind,windows,identity,totals:roleTotals(task,data.roster)};
}
