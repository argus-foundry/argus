import { node, errorMessage } from '../shared/dom.js';

export const PREVIEW_LIMIT = 16 * 1024 * 1024;
export function artifactPath(task, artifact) {
  const segments = typeof artifact.filename === 'string' ? artifact.filename.split('/') : [];
  if (!segments.length || segments.some(segment => !segment || segment === '.' || segment === '..' || /[\x00-\x1f\\]/.test(segment))) {
    throw new Error('This artifact has an invalid filename. Refresh the artifact list.');
  }
  return `/api/tasks/${encodeURIComponent(task.id)}/artifacts/${segments.map(encodeURIComponent).join('/')}`;
}
export async function previewBlob(response) {
  if (Number(response.headers.get('Content-Length')) > PREVIEW_LIMIT) {
    await response.body?.cancel();
    throw new Error('This artifact is too large to preview. Download it to view the full file.');
  }
  const reader = response.body?.getReader();
  if (!reader) return response.blob();
  const chunks = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > PREVIEW_LIMIT) { await reader.cancel(); throw new Error('This artifact is too large to preview. Download it to view the full file.'); }
      chunks.push(value);
    }
  } finally { reader.releaseLock(); }
  return new Blob(chunks, { type: response.headers.get('Content-Type') || 'application/octet-stream' });
}
export function safeHTML(text) {
  const template = document.createElement('template');
  template.innerHTML = text;
  const content = template.content;
  content.querySelectorAll('script,iframe,frame,object,embed,base,meta,link,form').forEach(item => item.remove());
  content.querySelectorAll('*').forEach(item => {
    for (const attribute of [...item.attributes]) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith('on') || ['srcdoc', 'action', 'formaction', 'ping', 'target', 'download'].includes(name)) item.removeAttribute(attribute.name);
      if (['href', 'xlink:href'].includes(name) && !attribute.value.startsWith('#')) item.removeAttribute(attribute.name);
    }
  });
  const policy = "default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'";
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer"></head><body>${template.innerHTML}</body></html>`;
}
export async function renderArtifact(container, artifact, blob, makeURL, isCurrent) {
  const type = artifact.type;
  if (['html', 'text', 'markdown'].includes(type)) {
    const text = await blob.text();
    if (!isCurrent()) return;
    if (type === 'html') {
      const frame = node('iframe');
      frame.setAttribute('sandbox', '');
      frame.referrerPolicy = 'no-referrer';
      frame.title = artifact.name || artifact.filename;
      frame.srcdoc = safeHTML(text);
      container.append(frame);
    } else container.append(node('pre', '', text));
  } else if (type === 'image') {
    const image = node('img');
    image.alt = artifact.name || artifact.filename;
    image.src = makeURL(blob);
    image.addEventListener('error', () => { if (isCurrent()) container.replaceChildren(errorMessage('This image could not be displayed. Use Download to open the file.', 'tool-note')); });
    container.append(image);
  } else if (type === 'pdf') {
    const frame = node('iframe');
    frame.setAttribute('sandbox', '');
    frame.title = artifact.name || artifact.filename;
    frame.src = makeURL(blob);
    container.append(frame, node('p', 'tool-note', 'If your browser cannot display this PDF, use Download.'));
  } else if (['audio', 'video'].includes(type)) {
    const player = node(type);
    player.controls = true;
    player.preload = 'metadata';
    player.src = makeURL(blob);
    player.addEventListener('error', () => { if (isCurrent()) container.append(errorMessage('This media could not be played here. Use Download to open the file.', 'tool-note')); }, { once: true });
    container.append(player);
  } else container.append(node('p', 'tool-note', 'Preview is unavailable for this file type. Use Download to open it.'));
}
export function sizeLabel(size) {
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
  if (size >= 1024) return `${Math.round(size / 1024)} KB`;
  return `${Number(size) || 0} bytes`;
}
