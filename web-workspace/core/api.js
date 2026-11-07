export function log(area, event, fields = {}) {
  const safe = Object.fromEntries(Object.entries(fields).filter(([key, value]) =>
    ['count', 'status', 'bytes', 'attempt', 'elapsedMs', 'stale'].includes(key) &&
    (typeof value === 'number' || typeof value === 'boolean')));
  console.debug(`[workspace:${area}] ${event}`, safe);
}

export class Api {
  #token = '';
  #reads = new Map();
  #selection;

  constructor(inputTask = () => null) { this.inputTask = inputTask; }

  invalidateReads() { this.#reads.clear(); }

  selectTask(id) {
    if (id === this.#selection) return;
    this.#selection = id;
    for (const path of this.#reads.keys()) if (path.endsWith('/links')) this.#reads.delete(path);
  }

  setToken(token) {
    this.#reads.clear();
    this.#token = token.trim();
  }

  async request(path, options = {}) {
    this.#validatePath(path);
    const { allowErrorResponse = false, ...requestOptions } = options;
    const headers = new Headers(requestOptions.headers);
    if (this.#token) headers.set('Authorization', `Bearer ${this.#token}`);
    let response;
    try {
      response = await fetch(path, { ...requestOptions, headers, cache: 'no-store', redirect: 'error' });
    } catch (error) {
      if (error.name === 'AbortError') throw error;
      log('api', 'connection failed');
      throw new Error('Cannot reach Argus. Check the local frontend server and daemon.');
    }
    if (!response.ok) {
      log('api', 'request failed', { status: response.status });
      if (allowErrorResponse) return response;
      const descriptions = {
        401: 'The token was not accepted. Reconnect with a valid token.',
        403: 'This token does not allow that action.',
        404: 'This task or resource is unavailable.',
        409: 'The task changed. Refresh its state before trying again.',
      };
      const error = new Error(descriptions[response.status] || `Argus request failed (${response.status}).`);
      error.status = response.status;
      throw error;
    }
    if (!['GET', 'HEAD'].includes((requestOptions.method || 'GET').toUpperCase()) &&
        /^\/api\/(settings|projects|backends)(?:\/|$)/.test(path)) this.#reads.clear();
    return response;
  }

  async json(path, options = {}) {
    const { revalidate = false, ...requestOptions } = options;
    const cached = (requestOptions.method || 'GET').toUpperCase() === 'GET' &&
      (['/api/backends', '/api/projects/full'].includes(path) || /^\/api\/tasks\/[^/]+\/links$/.test(path));
    if (!cached) return (await this.request(path, requestOptions)).json();
    requestOptions.signal?.throwIfAborted();
    let read = this.#reads.get(path);
    if (revalidate && read?.settled) read = null;
    if (!read) {
      read = { settled: false };
      read.promise = this.request(path, { ...requestOptions, signal: AbortSignal.timeout(15000) })
        .then(response => response.json()).finally(() => { read.settled = true; });
      this.#reads.set(path, read);
      read.promise.catch(() => { if (this.#reads.get(path) === read) this.#reads.delete(path); });
    }
    const signal = requestOptions.signal;
    if (!signal) return structuredClone(await read.promise);
    return new Promise((resolve, reject) => {
      const aborted = () => reject(signal.reason);
      signal.addEventListener('abort', aborted, { once: true });
      read.promise.then(value => { if (!signal.aborted) resolve(structuredClone(value)); }, reject)
        .finally(() => signal.removeEventListener('abort', aborted));
    });
  }

  shareURL(origin) {
    const url = new URL(origin);
    if (!this.#token) throw new Error('Reconnect before generating a share URL.');
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname))) || url.username || url.password || url.pathname !== '/' || url.search || url.hash)
      throw new Error('Use an HTTPS Argus origin or an HTTP loopback origin without credentials or a path.');
    url.pathname = '/share'; url.searchParams.set('text', '[Shortcut Input]'); url.searchParams.set('token', this.#token);
    return url.href;
  }

  streamUrl(path) {
    this.#validatePath(path);
    const url = new URL(path, location.origin);
    url.searchParams.set('token', this.#token);
    return url.href;
  }

  #validatePath(path) {
    if (typeof path !== 'string' || !path.startsWith('/api/') || path.startsWith('/api//') ||
        new URL(path, 'http://localhost').pathname.startsWith('/api/') === false) {
      throw new Error('Only existing relative Argus API paths are supported.');
    }
  }
}
