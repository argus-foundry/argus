export class AttentionPolicy {
 constructor(){this.pending=new Set();this.arrivals=[];this.overflow=[];}
 arrive(id,now=Date.now()){
  if(this.pending.has(id))return null;
  this.pending.add(id);this.arrivals=this.arrivals.filter(item=>now-item.at<5000&&item.id!==id);
  this.overflow=this.overflow.filter(taskID=>this.arrivals.some(item=>item.id===taskID));
  this.arrivals.push({id,at:now});
  if(this.arrivals.length<=3)return {kind:'individual',ids:[id]};
  this.overflow.push(id);return {kind:'summary',ids:[...this.overflow]};
 }
 reconcile(ids){for(const id of this.pending)if(!ids.has(id))this.pending.delete(id);this.overflow=this.overflow.filter(id=>ids.has(id));}
}
