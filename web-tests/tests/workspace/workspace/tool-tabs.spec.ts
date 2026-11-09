import {test,expect} from '@playwright/test';
import {readFileSync} from 'node:fs';
import {actorPage} from '../_lifecycle-fixture.ts';
const cleanup=[];test.afterEach(async()=>{for(const dispose of cleanup.splice(0))await dispose();});
for(const width of [290,340])test(`five tool tabs stay on one row at ${width}px with keyboard navigation`,async({browser})=>{
 const html=readFileSync('../web-workspace/index.html','utf8');const tabs=html.match(/<div class="tool-tabs"[^]*?<\/div>/)[0].replace(' hidden>Inbox','>Inbox');
 const page=await actorPage(browser,cleanup,`<link rel="stylesheet" href="/workspace/styles.css"><style>body{display:block}#fixture{width:${width}px;margin:20px}</style><div id="fixture">${tabs}</div>`);
 await page.evaluate(async()=>{const {tabs}=await import('/shared/dom.js');tabs(document.querySelector('.tool-tabs'),()=>{});});
 await page.locator('#fixture').screenshot({path:`../.context/tool-tabs-${process.env.THEIA_SCREENSHOT_PHASE||'after'}-${width}.png`});
 const tops=await page.getByRole('tab').evaluateAll(tabs=>tabs.map(tab=>tab.getBoundingClientRect().top));expect(new Set(tops).size).toBe(1);
 await expect(page.getByRole('tab',{name:'Agent messages',exact:true})).toHaveText('Messages');
 await page.getByRole('tab',{name:'Changes',exact:true}).focus();await page.keyboard.press('End');await expect(page.getByRole('tab',{name:'Inbox',exact:true})).toBeFocused();await page.keyboard.press('ArrowRight');await expect(page.getByRole('tab',{name:'Changes',exact:true})).toBeFocused();
});
