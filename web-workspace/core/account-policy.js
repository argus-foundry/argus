export function creationAccounts(accounts, backends, backend) {
 const kind=backends.find(item=>item.name===backend)?.kind;
 return accounts.filter(account=>account.name==='default'||(['claude','codex'].includes(kind)&&account.supports?.[kind]));
}
export function creationAccount(accounts,picked){
 if(accounts.length<2)return null;
 return accounts.find(account=>account.name===picked)?.name||accounts.find(account=>account.is_default)?.name||accounts.find(account=>account.name==='default')?.name||accounts[0]?.name||null;
}
export function moveAccounts(task, accounts, backends) {
 if(backends.find(backend=>backend.name===task.backend)?.kind!=='claude')return [];
 return accounts.filter(account=>account.name&&account.supports?.claude&&account.name!==(task.account||'default'));
}
