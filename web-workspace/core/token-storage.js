export const TOKEN_KEY = 'argus-workspace-token';

export class TokenStorage {
  constructor(storage = () => window.localStorage) { this.storage = storage; }

  read() {
    try { return this.storage().getItem(TOKEN_KEY) || ''; }
    catch { return ''; }
  }

  connected(token, remember) {
    if (!remember) { this.clear(); return; }
    try { this.storage().setItem(TOKEN_KEY, token); }
    catch { /* Storage may be disabled; the active API token stays in memory. */ }
  }

  rejected(error) {
    if (![401, 403].includes(error.status)) return false;
    this.clear();
    return true;
  }

  clear() {
    try { this.storage().removeItem(TOKEN_KEY); }
    catch { /* Disabled storage must not prevent disconnect. */ }
  }
}
