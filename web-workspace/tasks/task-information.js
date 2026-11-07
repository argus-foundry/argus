export function safeTaskLinks(links=[]){return links.filter(link=>{try{const url=new URL(link.url);return ['http:','https:'].includes(url.protocol)&&!url.username&&!url.password;}catch{return false;}});}
export function resolvePR(entries = [], links = []) {
 const value = entries.find(entry => entry.namespace === 'pr' && entry.key === 'url')?.value;
 const cached = safeTaskLinks([{url:value}])[0];
 return cached?.url || safeTaskLinks(links).find(link => link.isPR)?.url || null;
}
export function fileCopyPath(worktree,path){return worktree?.startsWith('/')?worktree.replace(/\/$/,'')+'/'+path:path;}
export function changedFileTree(files){
 const roots=[],nodes=new Map();
 for(const file of files){const parts=file.path.replace(/\/$/,'').split('/');let siblings=roots,prefix='';
  for(let index=0;index<parts.length;index++){prefix+=(prefix?'/':'')+parts[index];let entry=nodes.get(prefix);if(!entry){entry={name:parts[index],path:prefix,directory:index<parts.length-1||file.directory,status:'',children:[]};nodes.set(prefix,entry);siblings.push(entry);}if(index===parts.length-1)entry.status=file.status;siblings=entry.children;}
 }
 return roots;
}
