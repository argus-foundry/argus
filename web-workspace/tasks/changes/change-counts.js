import {diffLines} from './git.js';
import {log} from '../../core/api.js';

export function diffCounts(diff='') {
  if (!diff) return {kind:'unavailable'};
  if (/^Binary files |^GIT binary patch/m.test(diff)) return {kind:'binary'};
  if (!/^diff --git /m.test(diff)) return {kind:'unavailable'};
  const counts={added:0,removed:0};
  for (const line of diffLines(diff)) {
    if (line.kind==='added') counts.added++;
    if (line.kind==='removed') counts.removed++;
  }
  return counts;
}

export function statTotals(status) {
  let found=false,added=0,removed=0;
  for(const stat of [status.BranchDiff,status.Diff])for(const line of (stat||'').split('\n')) {
    if(!/^\s*\d+ files? changed(?:,|$)/.test(line))continue;
    found=true;added+=Number(line.match(/(\d+) insertions?\(\+\)/)?.[1]||0);removed+=Number(line.match(/(\d+) deletions?\(-\)/)?.[1]||0);
  }
  return found?{added,removed}:null;
}

export class ChangedFileCounts {
  constructor(api,task,signal,apply) {Object.assign(this,{api,task,signal,apply,results:new Map(),queue:[],active:0,revisions:new Map()});}
  invalidate(path) {this.revisions.set(path,(this.revisions.get(path)||0)+1);this.results.delete(path);this.queue=this.queue.filter(item=>item!==path);}
  add(files) {
    if (this.signal.aborted) return;
    for (const file of files) {
      if (this.results.has(file.path)) continue;
      this.results.set(file.path,file.directory?{kind:'directory'}:null);
      if (file.directory) this.apply(file.path,this.results.get(file.path));
      else this.queue.push(file.path);
    }
    this.pump();
  }
  pump() {
    while (!this.signal.aborted&&this.active<4&&this.queue.length) {
      const path=this.queue.shift();this.active++;void this.read(path);
    }
  }
  async read(path) {
    const revision=this.revisions.get(path)||0;
    let counts;
    try {
      const result=await this.api.json(`/api/tasks/${encodeURIComponent(this.task.id)}/git/diff?path=${encodeURIComponent(path)}`,{signal:this.signal});
      counts=diffCounts(result.Diff);if (!this.signal.aborted) log('change-counts','loaded',{status:counts.kind||'text'});
    } catch(error) {
      counts={kind:'unavailable'};if (!this.signal.aborted) log('change-counts','load failed',{status:error.status||0});
    } finally {this.active--;}
    if (this.signal.aborted) return;
    if(revision===(this.revisions.get(path)||0)){this.results.set(path,counts);this.apply(path,counts);}this.pump();
  }
}
