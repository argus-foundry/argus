export function gitPath(value) {
  const path = value;
  if (!path.startsWith('"') || !path.endsWith('"')) return path;
  const bytes = [];
  for (let index = 1; index < path.length - 1; index++) {
    const char = path[index];
    if (char !== '\\') {
      const point = path.codePointAt(index);
      bytes.push(...new TextEncoder().encode(String.fromCodePoint(point)));
      if (point > 0xffff) index++;
      continue;
    }
    const escaped = path[++index];
    if (/[0-7]/.test(escaped)) {
      let octal = escaped;
      while (octal.length < 3 && /[0-7]/.test(path[index + 1] || 'x')) octal += path[++index];
      bytes.push(parseInt(octal, 8));
    } else bytes.push(...new TextEncoder().encode(({ t: '\t', n: '\n', r: '\r', '"': '"', '\\': '\\' })[escaped] ?? escaped));
  }
  return new TextDecoder().decode(new Uint8Array(bytes));
}
function destination(value) {
  const match = value.match(/^("(?:[^"\\]|\\.)*"|.*?) -> (.*)$/);
  return gitPath(match ? match[2] : value);
}
export function changedFiles(status) {
  const files = new Map();
  for (const line of (status.BranchFiles || '').split('\n')) {
    const parts = line.split('\t');
    if (parts.length < 2) continue;
    const path = gitPath(parts.at(-1));
    if (path) files.set(path, { path, status: parts[0], directory: path.endsWith('/') });
  }
  for (const line of (status.Status || '').split('\n')) {
    if (line.length < 4) continue;
    const state = line.slice(0, 2).trim();
    const path = /[RC]/.test(state) ? destination(line.slice(3)) : gitPath(line.slice(3));
    if (path) files.set(path, { path, status: state, directory: path.endsWith('/') });
  }
  return [...files.values()].sort((left, right) => left.path.localeCompare(right.path));
}
export function diffLines(diff, limit = Infinity) {
  let oldLine = 0, newLine = 0, inHunk = false;
  const lines = [];
  let start = 0;
  while (start <= diff.length && lines.length < limit) {
    const end = diff.indexOf('\n', start);
    const text = diff.slice(start, end === -1 ? diff.length : end);
    const hunk = text.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    if (hunk) { oldLine = Number(hunk[1]); newLine = Number(hunk[2]); inHunk = true; }
    if (text.startsWith('diff ')) inHunk = false;
    let kind = 'meta', oldNumber = '', newNumber = '';
    if (inHunk && !hunk) {
      if (text.startsWith('+')) { kind = 'added'; newNumber = newLine++; }
      else if (text.startsWith('-')) { kind = 'removed'; oldNumber = oldLine++; }
      else if (text.startsWith(' ')) { kind = 'context'; oldNumber = oldLine++; newNumber = newLine++; }
    }
    lines.push({ text, kind, oldNumber, newNumber, index: lines.length });
    if (end === -1) break;
    start = end + 1;
  }
  return lines;
}
export function splitLines(lines) {
  const result = [];
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    if (line.kind === 'removed' || line.kind === 'added') {
      const removed = [], added = [];
      while (index < lines.length && ['removed', 'added'].includes(lines[index].kind)) {
        (lines[index].kind === 'removed' ? removed : added).push(lines[index++]);
      }
      index--;
      for (let item = 0; item < Math.max(removed.length, added.length); item++) result.push({ left: removed[item], right: added[item] });
    } else result.push({ left: line, right: line });
  }
  return result;
}
