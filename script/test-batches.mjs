#!/usr/bin/env node
// Run large Go suites within the existing per-process hang guard. Every
// discovered top-level test must emit exactly one run event across the batches.
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { pathToFileURL } from 'node:url';

export function discoverTests(output) {
  const names = output.split(/\r?\n/).filter(line => /^Test[\p{L}\p{N}_]*$/u.test(line));
  if (!names.length || new Set(names).size !== names.length) throw new Error('Empty or duplicate test inventory');
  return names;
}

export function batches(names, size) {
  if (!Number.isInteger(size) || size < 1) throw new Error('Batch size must be a positive integer');
  return Array.from({ length: Math.ceil(names.length / size) }, (_, i) => names.slice(i * size, (i + 1) * size));
}

export function pattern(names) {
  return `^(${names.map(name => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})$`;
}

export function executedTests(output) {
  return output.split(/\r?\n/).filter(Boolean).map(line => JSON.parse(line))
    .filter(event => event.Action === 'run' && event.Test && !event.Test.includes('/')).map(event => event.Test);
}

export function verifyRuns(expected, actual) {
  if (new Set(actual).size !== actual.length || [...expected].sort().join('\n') !== [...actual].sort().join('\n')) {
    throw new Error('Test inventory mismatch: missing, unexpected or repeated top-level tests');
  }
}

export function parseProfile(text) {
  const [header, ...lines] = text.trim().split(/\r?\n/);
  if (!/^mode: (set|count|atomic)$/.test(header)) throw new Error('Invalid coverage mode');
  const blocks = lines.filter(Boolean).map(line => {
    const match = /^(.+):(\d+\.\d+,\d+\.\d+)\s+(\d+)\s+(\d+)$/.exec(line);
    if (!match) throw new Error(`Invalid coverage block: ${line}`);
    return { file: match[1], location: match[2], statements: match[3], count: BigInt(match[4]) };
  });
  return { header, blocks };
}

export function mergeProfiles(texts) {
  const profiles = texts.map(parseProfile);
  if (!profiles.length || profiles.some(p => p.header !== profiles[0].header)) throw new Error('Coverage modes differ');
  const blocks = new Map();
  for (const p of profiles) for (const b of p.blocks) {
    const key = `${b.file}:${b.location}`;
    const previous = blocks.get(key);
    if (previous && previous.statements !== b.statements) throw new Error('Coverage sources differ');
    blocks.set(key, { ...b, count: profiles[0].header === 'mode: set' ? (b.count || previous?.count || 0n) : b.count + (previous?.count || 0n) });
  }
  return profiles[0].header + '\n' + [...blocks.entries()].sort(([a], [b]) => a.localeCompare(b))
    .map(([key, b]) => `${key} ${b.statements} ${b.count}`).join('\n') + '\n';
}

export function replacePackages(base, replacements) {
  const parsed = parseProfile(base);
  const packages = new Set(replacements.map(r => r.pkg));
  const parent = file => file.slice(0, file.lastIndexOf('/'));
  const kept = parsed.header + '\n' + parsed.blocks.filter(b => !packages.has(parent(b.file)))
    .map(b => `${b.file}:${b.location} ${b.statements} ${b.count}`).join('\n') + '\n';
  for (const r of replacements) {
    const replacement = parseProfile(r.text);
    if (!replacement.blocks.length || replacement.blocks.some(b => parent(b.file) !== r.pkg)) throw new Error('Replacement has unexpected package');
  }
  return mergeProfiles([kept, ...replacements.map(r => r.text)]);
}

function go(args) {
  const result = spawnSync('go', args, { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
  if (result.error) throw result.error;
  return result;
}

function save(path, text) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, text, { mode: 0o600 });
}

function run(pkg, out, size = '40') {
  const listed = go(['test', '-race', '-list', '^Test', pkg]);
  if (listed.status !== 0) throw new Error(listed.stderr || listed.stdout);
  const names = discoverTests(listed.stdout);
  const groups = batches(names, Number(size));
  const dir = mkdtempSync(join(tmpdir(), 'argus-test-batches-'));
  const profiles = [], actual = [], results = [];
  let failed = false;
  const started = Date.now();
  for (const [i, group] of groups.entries()) {
    const profile = join(dir, `${i}.cover`);
    const log = join(dir, `${i}.jsonl`);
    const args = ['test', '-json', '-race', '-count=1', '-timeout=300s', `-coverprofile=${profile}`, '-run', pattern(group), pkg];
    const start = Date.now();
    const result = go(args);
    save(log, result.stdout + result.stderr);
    const ran = executedTests(result.stdout);
    actual.push(...ran);
    try { verifyRuns(group, ran); } catch { failed = true; }
    if (result.status !== 0) failed = true;
    try { profiles.push(readFileSync(profile, 'utf8')); } catch { failed = true; }
    results.push({ tests: group, executed: ran, status: result.status, seconds: (Date.now() - start) / 1000, log });
    console.log(`${pkg} batch ${i + 1}/${groups.length}: ${result.status === 0 ? 'PASS' : 'FAIL'} (${ran.length}/${group.length} tests, ${results.at(-1).seconds}s) log=${log}`);
  }
  try { verifyRuns(names, actual); } catch { failed = true; }
  save(out, mergeProfiles(profiles));
  save(out + '.json', JSON.stringify({ pkg, tests: names, executed: actual, flags: ['-race', '-count=1', '-timeout=300s'], results, seconds: (Date.now() - started) / 1000, passed: !failed }, null, 2) + '\n');
  console.log(`${failed ? 'FAIL' : 'PASS'}: ${actual.length}/${names.length} top-level tests; coverage=${out}; manifest=${out}.json`);
  if (failed) process.exitCode = 1;
}

export function main(args) {
  if (args[0] === 'run' && args.length >= 3 && args.length <= 4) return run(args[1], args[2], args[3]);
  if (args[0] === 'merge' && args.length >= 4) {
    const replacements = args.slice(3).map(arg => {
      const split = arg.indexOf('=');
      if (split < 1) throw new Error('Replacement must be package=profile');
      return { pkg: arg.slice(0, split), text: readFileSync(arg.slice(split + 1), 'utf8') };
    });
    return save(args[1], replacePackages(readFileSync(args[2], 'utf8'), replacements));
  }
  throw new Error('Usage: node script/test-batches.mjs run <package> <coverage> [batch-size] | merge <output> <base> <module/package=coverage>...');
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { main(process.argv.slice(2)); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
