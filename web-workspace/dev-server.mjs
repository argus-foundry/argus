import http from 'node:http';
import { createReadStream } from 'node:fs';
import { stat, realpath } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const clientRoot = path.dirname(fileURLToPath(import.meta.url));
const staticRoot = path.resolve(clientRoot, '../internal/api/static');
const vendorRoot = path.join(staticRoot, 'vendor');
const appIcons = new Set(['/icon-192.png', '/apple-touch-icon.png']);
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css',
  '.svg': 'image/svg+xml', '.png': 'image/png', '.json': 'application/json' };
const hopHeaders = ['connection', 'keep-alive', 'proxy-authenticate', 'proxy-authorization',
  'te', 'trailer', 'transfer-encoding', 'upgrade'];

function end(response, status, message) {
  response.writeHead(status, { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' });
  response.end(message);
}

function safeHeaders(original) {
  const headers = { ...original };
  for (const key of String(headers.connection || '').split(',')) delete headers[key.trim().toLowerCase()];
  for (const key of hopHeaders) delete headers[key];
  return headers;
}

export function daemonURL(value) {
  const url = new URL(value);
  if (url.protocol !== 'http:' || !['127.0.0.1', '[::1]'].includes(url.hostname) ||
      url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('The development proxy requires an HTTP loopback daemon origin without credentials or a path.');
  }
  return url;
}

function proxy(request, response, daemon) {
  const headers = safeHeaders(request.headers);
  headers.host = daemon.host;
  delete headers.cookie;
  const upstream = http.request({ hostname: daemon.hostname.replace(/^\[|\]$/g, ''), port: daemon.port || 80,
    method: request.method, path: request.url, headers }, incoming => {
    const forwarded = safeHeaders(incoming.headers);
    delete forwarded['set-cookie'];
    forwarded['content-security-policy'] = [forwarded['content-security-policy'], 'sandbox'].filter(Boolean).join(', ');
    forwarded['x-content-type-options'] = 'nosniff';
    response.writeHead(incoming.statusCode, forwarded);
    response.flushHeaders();
    incoming.pipe(response);
    incoming.on('error', () => response.destroy());
    response.on('close', () => incoming.destroy());
  });
  upstream.on('error', () => {
    if (!response.headersSent) end(response, 502, 'The configured Argus daemon is unavailable.');
    else response.destroy();
  });
  upstream.setTimeout(65_000, () => upstream.destroy());
  request.on('aborted', () => upstream.destroy());
  response.on('close', () => upstream.destroy());
  request.pipe(upstream);
}

async function serve(request, response) {
  if (!['GET', 'HEAD'].includes(request.method)) return end(response, 405, 'Static assets are read only.');
  let pathname;
  try { pathname = decodeURIComponent(new URL(request.url, 'http://localhost').pathname); }
  catch { return end(response, 400, 'Invalid path.'); }
  const vendor = pathname.startsWith('/vendor/');
  const root = vendor ? vendorRoot : appIcons.has(pathname) ? staticRoot : clientRoot;
  const relative = vendor ? pathname.slice('/vendor/'.length) : pathname === '/' ? 'index.html' : pathname.slice(1);
  if (vendor && !['xterm.js', 'xterm.css', 'xterm-addon-fit.js'].includes(relative)) {
    return end(response, 404, 'Not found.');
  }
  const candidate = path.resolve(root, relative);
  if (!candidate.startsWith(`${root}${path.sep}`) || !types[path.extname(candidate)]) {
    return end(response, 404, 'Not found.');
  }
  try {
    const resolved = await realpath(candidate);
    if (!resolved.startsWith(`${root}${path.sep}`)) return end(response, 404, 'Not found.');
    const info = await stat(resolved);
    if (!info.isFile()) return end(response, 404, 'Not found.');
    if (path.extname(candidate) === '.html') {
      response.setHeader('Content-Security-Policy', "frame-ancestors 'none'");
      response.setHeader('X-Frame-Options', 'DENY');
    }
    response.writeHead(200, { 'Content-Type': `${types[path.extname(candidate)]}; charset=utf-8`,
      'Content-Length': info.size, 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff',
      'Referrer-Policy': 'no-referrer' });
    if (request.method === 'HEAD') return response.end();
    createReadStream(resolved).on('error', () => response.destroy()).pipe(response);
  } catch { end(response, 404, 'Not found.'); }
}

export function createWorkspaceServer({ api = 'http://127.0.0.1:7743' } = {}) {
  const daemon = daemonURL(api);
  return http.createServer((request, response) => {
    if (!/^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/.test(request.headers.host || '')) {
      return end(response, 403, 'Use the loopback workspace address.');
    }
    const origin = request.headers.origin;
    const expected = `http://${request.headers.host}`;
    const apiRequest = request.url.startsWith('/api/');
    const pageNavigation = !apiRequest && ['GET', 'HEAD'].includes(request.method) &&
      request.headers['sec-fetch-mode'] === 'navigate' && request.headers['sec-fetch-dest'] === 'document';
    if (origin && origin !== expected && !pageNavigation) return end(response, 403, 'Use the local workspace origin.');
    if (request.headers['sec-fetch-site'] === 'cross-site' && !pageNavigation) return end(response, 403, 'Use the local workspace origin.');
    if (request.url.startsWith('/api/')) return proxy(request, response, daemon);
    if (request.url === '/original') {
      response.writeHead(302, { Location: daemon.href, 'Cache-Control': 'no-store', 'Referrer-Policy': 'no-referrer' });
      return response.end();
    }
    void serve(request, response);
  });
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const option = (name, fallback) => {
    const index = args.indexOf(name);
    return index === -1 ? fallback : args[index + 1];
  };
  try {
    if (args.some((value, index) => index % 2 === 0 && !['--port', '--api'].includes(value)) || args.length % 2) {
      throw new Error('Usage: node web-workspace/dev-server.mjs [--port 7750] [--api http://127.0.0.1:7743]');
    }
    const port = Number(option('--port', '7750'));
    if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Invalid frontend port.');
    const server = createWorkspaceServer({ api: option('--api', 'http://127.0.0.1:7743') });
    server.on('error', error => { console.error(`Workspace server failed (${error.code || 'unknown'}).`); process.exitCode = 1; });
    server.listen(port, '127.0.0.1', () => console.log(`Optional Argus workspace at http://127.0.0.1:${port}`));
    for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => { server.closeAllConnections(); server.close(); });
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
