export function accountList(data){return Array.isArray(data)?data:Array.isArray(data?.accounts)?data.accounts:[];}
export {creationAccounts, creationAccount} from '../../core/account-policy.js';
