import {log} from '../../core/api.js';

export class AttentionTab {
 constructor(){
  this.icon=document.querySelector('link[rel~="icon"]');
  this.original=this.icon.getAttribute('href');this.count=null;
 }
 update(count){
  if(count===this.count)return;
  this.count=count;document.title=(count?`(${count}) `:'')+'Argus';
  this.draw();
  if(count&&!this.image){
   this.image=new Image();
   this.image.onload=()=>{log('attention','favicon image loaded');this.draw();};
   this.image.onerror=()=>{log('attention','favicon image unavailable');this.icon.href=this.original;};
   this.image.src=this.original;
  }
 }
 draw(){
  const badge=this.count&&this.image?.naturalWidth?Math.min(this.count,99):0;
  if(badge===this.drawnBadge)return;
  this.icon.href=this.original;
  this.drawnBadge=0;
  if(!badge)return;
  try{
   const canvas=document.createElement('canvas');canvas.width=canvas.height=192;
   const context=canvas.getContext('2d');context.drawImage(this.image,0,0,192,192);
   context.fillStyle='#dc2626';context.beginPath();context.arc(148,148,44,0,Math.PI*2);context.fill();
   context.fillStyle='#fff';context.font='bold 48px sans-serif';context.textAlign='center';context.textBaseline='middle';
   context.fillText(String(badge),148,150);
   this.icon.href=canvas.toDataURL('image/png');
   this.drawnBadge=badge;
  }catch{log('attention','favicon badge unavailable');}
 }
}
