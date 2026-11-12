export function comparisonText(comparison) {
  if(!comparison)return 'Comparison unavailable from the daemon.';
  const {requested_base:requested,resolved_base:resolved,merge_base:merge,fallback_reason:fallback}=comparison;
  const branch=merge&&resolved?`Branch vs merge-base ${merge} of ${resolved}`:'Branch comparison unavailable';
  return `${requested?`Requested ${requested}. `:''}Uncommitted vs HEAD · ${branch} · Untracked contents.${fallback?` ${fallback}. Effective base: ${resolved||'none (branch comparison unavailable)'}.`:''}`;
}
