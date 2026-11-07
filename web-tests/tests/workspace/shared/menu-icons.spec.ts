import {test,expect} from '@playwright/test';
import {mkdir} from 'node:fs/promises';
import path from 'node:path';

const screenshots=path.resolve('../.context/menu-icons/after');
const cases=['task','pinned-task','archived-task','coordinator','nested-coordinator','file','relative-file','settings'];

test('all rendered menu items have one decorative leading icon in either palette',async({page})=>{
  await mkdir(screenshots,{recursive:true});
  await page.setViewportSize({width:1280,height:1000});
  await page.goto('/');
  await page.evaluate(()=>{document.querySelectorAll('body > *').forEach(item=>item.remove());});
  const failures=[];
  for(const theme of ['dark','light'])for(const kind of cases){
    await page.evaluate(async({kind,theme})=>{
      const {taskMenu}=await import('/workspace/rail/task-menu.js');
      const {fileRow}=await import('/tasks/changes/task-files.js');
      const {SettingsNavigation}=await import('/settings/settings-navigation.js');
      document.body.replaceChildren();
      document.documentElement.style.cssText=theme==='light'?'--background:#fff;--surface:#fafafa;--raised:#fff;--text:#222;--secondary:#555;--border:#ddd;--error:#b42318;--shadow:#0002;color-scheme:light':'';
      const rail=document.createElement('aside');rail.style.cssText='width:220px;padding:12px;position:relative';document.body.append(rail);
      const task={id:'icons',name:'Menu icon review',project:'fixture',status:'complete',branch:'menu-icons',worktree_path:'/tmp/menu-icons',pinned:kind==='pinned-task',archived:kind==='archived-task'};
      if(kind==='settings'){
        const nav=new SettingsNavigation({},()=>{},()=>{});nav.projects=[{name:'fixture'}];nav.render();rail.append(nav.element);
      }else if(kind.includes('file')){
        if(kind==='relative-file')delete task.worktree_path;
        rail.append(fileRow(task,{path:'src/example.js',status:'M'},{openDiff:()=>{},expandDirectory:()=>{},counts:{added:1,removed:0}}));
      }else{
        const coordinator=kind.includes('coordinator')?{root:kind==='coordinator',pinned:false,kanban_status:'active'}:undefined;
        rail.append(...taskMenu(task,{settings:()=>{},quickAction:()=>{},coordinator,heraAction:()=>{},deleteTask:()=>{},selectTask:()=>{}}));
      }
      const trigger=rail.querySelector('[aria-haspopup=menu]');if(trigger)trigger.click();
    },{kind,theme});
    await page.screenshot({path:path.join(screenshots,`${kind}-${theme}.png`)});
    expect(await page.locator('[role=menuitem],.settings-nav-entry').count()).toBeGreaterThan(0);
    const errors=await page.locator('[role=menuitem],.settings-nav-entry').evaluateAll(items=>items.flatMap(item=>{
      if(item.getAttribute('data-section')?.startsWith('repository:'))return [];
      const icons=item.querySelectorAll('svg');
      return icons.length===1&&icons[0].getAttribute('aria-hidden')==='true'&&item.firstElementChild===icons[0]?[]:[`${item.textContent}: expected one leading decorative icon, found ${icons.length}`];
    }));
    failures.push(...errors.map(error=>`${kind}/${theme} ${error}`));
    for(const item of await page.locator('[role=menuitem],.settings-nav-entry:not([data-section^="repository:"])').all()){
      const icon=item.locator('svg');await expect(icon).toHaveAttribute('aria-hidden','true');
      expect(await icon.evaluate(svg=>getComputedStyle(svg).stroke)).toBe(await item.evaluate(item=>getComputedStyle(item).color));
      expect(await icon.evaluate(svg=>svg.getBoundingClientRect().width)).toBe(16);
      expect(await icon.evaluate(svg=>getComputedStyle(svg).strokeWidth)).toBe('1.5px');
      expect(await icon.evaluate(svg=>getComputedStyle(svg).fill)).toBe('none');
      if(await item.getAttribute('role')==='menuitem'){
        const first=page.locator('[role=menuitem]').first().locator('svg');
        expect(await icon.evaluate(svg=>svg.getBoundingClientRect().x)).toBe(await first.evaluate(svg=>svg.getBoundingClientRect().x));
      }
    }
    if(!kind.includes('file')&&kind!=='settings'){
      const danger=page.locator('[role=menuitem].danger-menu-item');
      await expect(danger).toHaveCount(1);
      expect(await danger.evaluate(item=>item.previousElementSibling?.getAttribute('role'))).toBe('separator');
      expect(await danger.evaluate(item=>{
        const probe=document.createElement('span');probe.style.color='var(--error)';item.append(probe);
        const expected=getComputedStyle(probe).color;probe.remove();return getComputedStyle(item).color===expected;
      })).toBe(true);
    }
    const trigger=page.locator('[aria-haspopup=menu]');
    if(await trigger.count()){
      await page.keyboard.press('End');await expect(page.locator('[role=menuitem]:not(:disabled)').last()).toBeFocused();
      await page.keyboard.press('Home');await expect(page.locator('[role=menuitem]:not(:disabled)').first()).toBeFocused();
      await page.keyboard.press('Escape');await expect(trigger).toBeFocused();
    }
  }
  expect(failures).toEqual([]);
});
