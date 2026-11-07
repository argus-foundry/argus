import {log} from '../../core/api.js';

export class AttentionTab {
 constructor(){
  this.icons=[...document.querySelectorAll('link[rel~="icon"]')].map(icon=>({icon,href:icon.getAttribute('href'),type:icon.getAttribute('type'),sizes:icon.getAttribute('sizes')}));
  this.source='/theia-icon-192.png';this.count=null;
 }
 update(count){
  if(count===this.count)return;
  this.count=count;document.title=(count?`(${count}) `:'')+'Theia';
  this.draw();
  if(count&&!this.image){
   this.image=new Image();
   this.image.onload=()=>{log('attention','favicon image loaded');this.draw();};
   this.image.onerror=()=>{log('attention','favicon image unavailable');this.restore();};
   this.image.src=this.source;
  }
 }
 restore(){
  for(const {icon,href,type,sizes} of this.icons){
   icon.href=href;
   for(const [name,value] of [['type',type],['sizes',sizes]]){
    if(value===null)icon.removeAttribute(name);else icon.setAttribute(name,value);
   }
  }
 }
 draw(){
  const badge=this.count&&this.image?.naturalWidth?Math.min(this.count,99):0;
  if(badge===this.drawnBadge)return;
  this.restore();
  this.drawnBadge=0;
  if(!badge)return;
  try{
   const canvas=document.createElement('canvas');canvas.width=canvas.height=192;
   const context=canvas.getContext('2d');context.drawImage(this.image,0,0,192,192);
   context.fillStyle='#dc2626';context.beginPath();context.arc(148,148,44,0,Math.PI*2);context.fill();
   context.fillStyle='#fff';context.font='bold 48px sans-serif';context.textAlign='center';context.textBaseline='middle';
   context.fillText(String(badge),148,150);
   const href=canvas.toDataURL('image/png');
   for(const {icon} of this.icons){icon.href=href;icon.setAttribute('type','image/png');icon.setAttribute('sizes','192x192');}
   this.drawnBadge=badge;
  }catch{log('attention','favicon badge unavailable');}
 }
}
