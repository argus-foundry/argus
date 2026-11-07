export function transferFiles(data) {
  if (data?.files?.length) return [...data.files];
  return [...(data?.items || [])].filter(item => item.kind === 'file').map(item => item.getAsFile?.()).filter(Boolean);
}

export function fileDrag(data) {
  return !!data && ([...(data.types || [])].includes('Files') || !!data.files?.length || [...(data.items || [])].some(item => item.kind === 'file'));
}

export function imageFile(file) { return file.type.startsWith('image/') || /\.(png|jpe?g|gif|webp|bmp|avif|heic|tiff?)$/i.test(file.name); }

export function validateFiles(files, existing = []) {
  const all = [...existing, ...files];
  if (!files.length) throw new Error('No files available. Try dragging the saved file from Finder.');
  if (all.length > 20) throw new Error('Attach up to 20 files per message.');
  if (all.some(file => file.size === 0)) throw new Error('Empty files cannot be uploaded.');
  if (all.some(file => file.size > 100 * 1024 * 1024)) throw new Error('Each file must be 100 MiB or smaller.');
  if (all.reduce((total, file) => total + file.size, 0) > 250 * 1024 * 1024) throw new Error('Attachments must total 250 MiB or less.');
}

export function uploadedPath(result) {
  const paths = result?.paths;
  if (!Array.isArray(paths) || paths.length !== 1 || typeof paths[0] !== 'string' || !/^\.\/\.context\/[^/\\\x00-\x1f\x7f]+$/.test(paths[0])) {
    throw new Error('Upload was not confirmed. Check Files before retrying.');
  }
  return paths[0];
}

export function attachmentMessage(text, paths) {
  const escaped = paths.map(path => path.replace(/([\s"'\\`$&;|<>(){}\[\]*?!#~^%=])/g, '\\$1'));
  return paths.length ? [text, ...escaped].filter(Boolean).join(' ') : text;
}
