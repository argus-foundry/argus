const valid = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
const percentage = value => valid(value) ? Math.max(0,Math.min(100,value)) : null;
export function formatSize(value) {
  if (!valid(value)) return 'Unavailable';
  const units=['B','KiB','MiB','GiB','TiB'];let i=0;
  while(value>=1024 && i<units.length-1){value/=1024;i++;}
  return `${i ? value.toFixed(1) : value} ${units[i]}`;
}
const pct = value => `${value.toFixed(0)}%`;
export function systemMetrics(m = {}) {
  const rows=[],add=(label,available,value,fill=null)=>rows.push({label,value:available?value:'Unavailable',percent:available?percentage(fill):null});
  add('CPU',m.cpu_avail && valid(m.cpu_percent),valid(m.cpu_percent)?pct(m.cpu_percent):'',m.cpu_percent);
  add('Load average',m.load_avail && [m.load1,m.load5,m.load15].every(valid),[m.load1,m.load5,m.load15].filter(valid).map(value=>value.toFixed(2)).join('  '));
  for(const [label,key] of [['Memory','mem'],['Swap','swap'],['Disk','disk']]) {
    const used=m[`${key}_used`],total=m[`${key}_total`],fill=m[`${key}_percent`];
    if(key==='swap' && m.swap_avail && total===0){add(label,true,'None');continue;}
    const available=m[`${key}_avail`] && [used,total,fill].every(valid);
    let value=available?`${formatSize(used)} / ${formatSize(total)} (${pct(fill)})`:'';
    if(key==='disk' && available && valid(m.disk_free))value+=` · ${formatSize(m.disk_free)} free`;
    add(label,available,value,fill);
  }
  add('Argus process',m.proc_avail && valid(m.proc_rss),`${formatSize(m.proc_rss)} RSS`);
  const sessions=m.sessions || {};add('Agent sessions',[sessions.running,sessions.idle].every(valid),`${sessions.running} running, ${sessions.idle} idle`);
  const seconds=m.uptime_sec,days=Math.floor(seconds/86400),hours=Math.floor(seconds%86400/3600),minutes=Math.floor(seconds%3600/60);
  add('Uptime',m.uptime_avail && valid(seconds),days?`${days}d ${hours}h`:hours?`${hours}h ${minutes}m`:`${minutes}m`);
  return rows;
}
