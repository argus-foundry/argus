import {test,expect} from '@playwright/test';
import {actorPage} from '../_lifecycle-fixture.ts';
const cleanup=[];test.afterEach(async()=>{for(const dispose of cleanup.splice(0))await dispose();});
test('Agents account list scopes Claude usage and reset time to valid matching accounts',async({browser})=>{
 const page=await actorPage(browser,cleanup,'<button class="settings-back">Back</button><div id="agents"></div>');let used=25;const future=new Date(Date.now()+86400000).toISOString();
 await page.route('**/api/backends',route=>route.fulfill({json:{backends:[]}}));
 await page.route('**/api/accounts',route=>route.fulfill({json:[{name:'work',supports:{claude:true,codex:true},usage:{five_hour_pct:used,five_hour_resets_at:future,weekly_pct:50,weekly_resets_at:future}},{name:'expired',supports:{claude:true},usage:{five_hour_pct:25,five_hour_resets_at:'2020-01-01T00:00:00Z'}},{name:'missing',supports:{claude:true}},{name:'codex',supports:{claude:false,codex:true},usage:{five_hour_pct:10,five_hour_resets_at:future}}]}));
 await page.evaluate(async()=>{const {Api}=await import('/core/api.js');const {AgentSettings}=await import('/settings/agents/settings-agents.js');window.settings=new AgentSettings({api:new Api(),host:document.querySelector('#agents')});});
 const account=name=>page.locator('article').filter({has:page.getByRole('heading',{name,exact:true})});
 await expect(account('work')).toContainText('Claude usage');await expect(account('work')).toContainText('25% used');await expect(account('work')).toContainText('50% used');await expect(account('work')).toContainText('resets');
 for(const name of ['expired','missing']){await expect(account(name)).toContainText('Claude usage unavailable');await expect(account(name)).not.toContainText('0% used');}
 await expect(account('codex')).not.toContainText('Claude usage');used=35;await page.getByRole('button',{name:'Refresh agents'}).click();await expect(account('work')).toContainText('35% used');
});
