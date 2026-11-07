export function focusDOM(t) {
  const original = {document: globalThis.document, IntersectionObserver: globalThis.IntersectionObserver};
  class Element {
    constructor(tag) { this.tagName=tag; this.children=[]; this.dataset={}; this.attributes={}; this.className=''; this.scrollTop=0; }
    append(...items) { for(const item of items) { item.remove(); item.parentElement=this; this.children.push(item); } }
    prepend(...items) { this.append(...items); this.children.unshift(...this.children.splice(-items.length)); }
    remove() { if(this.parentElement) { const parent=this.parentElement; parent.children.splice(parent.children.indexOf(this),1); this.parentElement=null; if(this.contains(document.activeElement)) document.activeElement=document.body; } }
    replaceChildren(...items) { for(const child of [...this.children])child.remove(); this.append(...items); }
    contains(item) { return item===this || this.children.some(child=>child.contains(item)); }
    get isConnected() { return document.body.contains(this); }
    setAttribute(key,value) { this.attributes[key]=String(value); }
    getAttribute(key) { return this.attributes[key]??null; }
    addEventListener() {}
    focus() { document.activeElement=this; }
    matches(selector) {
      if(selector.includes(','))return selector.split(',').some(part=>this.matches(part.trim()));
      if(selector.includes(':popover-open'))return false;
      const attribute=selector.match(/\[([^=\]]+)(?:=["']?([^"'\]]+)["']?)?\]/);
      if(attribute && !(attribute[1].startsWith('data-') ? attribute[1].slice(5).replace(/-([a-z])/g,(_,letter)=>letter.toUpperCase()) in this.dataset : attribute[1] in this.attributes))return false;
      const classes=[...selector.matchAll(/\.([\w-]+)/g)].map(match=>match[1]);
      const tag=selector.match(/^[a-z]+/)?.[0];
      return (!tag||tag===this.tagName)&&classes.every(name=>this.className.split(' ').includes(name));
    }
    closest(selector) { return this.matches(selector)?this:this.parentElement?.closest(selector); }
    querySelectorAll(selector) { return this.children.flatMap(child=>[...(child.matches(selector)?[child]:[]),...child.querySelectorAll(selector)]); }
    querySelector(selector) { return this.querySelectorAll(selector)[0]||null; }
  }
  const body=new Element('body'), tab=new Element('button');tab.id='tool-changes';body.append(tab);
  globalThis.document={body,activeElement:tab,createElement:tag=>new Element(tag),createElementNS:(_,tag)=>new Element(tag),createTextNode:()=>new Element('text'),getElementById:id=>body.querySelectorAll('button').find(item=>item.id===id)};
  globalThis.IntersectionObserver=class {observe(){} disconnect(){}};
  t.after(()=>Object.assign(globalThis,original));
  return {body,tab,element:tag=>new Element(tag)};
}
