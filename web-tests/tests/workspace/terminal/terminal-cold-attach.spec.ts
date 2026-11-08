import {test, expect} from '@playwright/test';

for (const kind of ['claude','','codex']) for (const warm of [false,true]) for (const source of ['snapshot','first-live-frame']) {
  test(`real terminal focus with kind=${JSON.stringify(kind)} warm=${warm} source=${source}`,async({page})=>{
    let release;
    const gate=new Promise<void>(resolve=>{release=resolve;});
    const writes=[];
    await page.route('**/api/**',async route=>{
      const path=new URL(route.request().url()).pathname;
      if(path==='/api/backends'){
        if(!warm)await gate;
        return route.fulfill({json:{backends:[{name:'alias',kind}]}});
      }
      if(path.endsWith('/input')){writes.push(route.request().postData());return route.fulfill({body:''});}
      if(path.endsWith('/size'))return route.fulfill({json:{cols:80,rows:24}});
      if(path.endsWith('/output'))return route.fulfill({body:source==='snapshot'?'\x1b[?1004h':'',headers:{'X-Output-Total':'0'}});
      return route.fulfill({json:{}});
    });
    await page.goto('/');
    try {
      await page.evaluate(async({warm})=>{
        const {Api}=await import('/core/api.js');
        const {AgentTerminal}=await import('/terminal/terminal.js');
        window.EventSource=class {
          constructor(){window.focusSource=this;queueMicrotask(()=>this.onopen?.());}
          addEventListener(){} close(){}
        };
        const host=document.createElement('div');host.style.cssText='height:400px;width:900px';
        const composer=document.createElement('textarea');composer.id='draft';document.body.replaceChildren(host,composer);
        const task={id:'cold-attach',backend:'alias',live:true,idle:false,status:'in_progress'};
        const api=new Api(()=>task);
        if(warm)await api.json('/api/backends');
        window.focusTerminal=new AgentTerminal(api,host);
        await window.focusTerminal.attach(task);
      },{warm});
      await expect.poll(()=>page.evaluate(()=>window.focusTerminal.interactive)).toBe(true);
      if(source==='first-live-frame'){
        await page.evaluate(()=>window.focusSource.onmessage({data:btoa('\x1b[?1004h')}));
        await expect.poll(()=>page.evaluate(()=>window.focusTerminal.stream.pendingWrites.size)).toBe(0);
      }
      await page.evaluate(()=>window.focusTerminal.input.chain);
      expect(writes).toEqual(warm&&kind==='codex'&&source==='first-live-frame'?['\x1b[O']:[]);
      await page.evaluate(()=>{window.focusTerminal.term.focus();document.getElementById('draft').focus();});
      await page.evaluate(()=>window.focusTerminal.input.chain);
      const before=warm&&kind==='codex'?(source==='first-live-frame'?['\x1b[O','\x1b[I','\x1b[O']:['\x1b[I','\x1b[O']):['\x1b[I'];
      expect(writes).toEqual(before);
      release();
      await expect.poll(()=>page.evaluate(()=>typeof window.focusTerminal.input.claudeFamily)).toBe('boolean');
      await page.evaluate(()=>window.focusTerminal.input.chain);
      expect(writes).toEqual(!warm&&kind==='codex'?['\x1b[I','\x1b[O']:before);
    } finally {
      release();
      await page.evaluate(()=>window.focusTerminal?.destroy());
    }
  });
}
