import {TaskHeaderLinks} from '../../tasks/task-links.js';

export function fixture(t, read) {
 const original=globalThis.document;
 class Element {
  constructor(tag){this.tagName=tag;this.children=[];this.dataset={};this.attributes={};}
  append(...items){this.children.push(...items);}
  replaceChildren(...items){this.children=items;this.textContent='';}
  contains(target){return this===target||this.children.some(child=>child.contains?.(target));}
  after(){}
  addEventListener(){}
  setAttribute(key,value){this.attributes[key]=value;}
  removeAttribute(key){delete this.attributes[key];}
  querySelector(selector){return this.children.find(child=>child.tagName===selector)||this.children.map(child=>child.querySelector?.(selector)).find(Boolean)||null;}
 }
 globalThis.document={createElement:tag=>new Element(tag),createTextNode:text=>({textContent:text}),getElementById:()=>new Element('h1')};
 t.after(()=>{globalThis.document=original;});
 const reads=[];
 const header=new TaskHeaderLinks({json(path,options){reads.push({path,...options});return read(path,options);}});
 t.after(()=>header.clear());
 return {header,reads};
}
export const task={id:'a',branch:'feature',pr_state:''};
export const metadata=url=>({entries:url?[{namespace:'pr',key:'url',value:url}]:[]});
