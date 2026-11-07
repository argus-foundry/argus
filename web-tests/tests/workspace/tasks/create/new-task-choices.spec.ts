import {test,expect} from '@playwright/test';

test.beforeEach(async ({page})=>{
  await page.goto('/');
  await page.evaluate(async()=>{
    const {NewTaskView}=await import('/tasks/create/new-task.js');
    const backends=[{name:'assistant',model:'sonnet',models:['sonnet','opus']},{name:'coder',model:'gpt-default',models:['gpt-custom']},{name:'custom',model:'provider-default',models:[]}];
    document.getElementById('new-task-view')?.remove();
    document.getElementById('connect-dialog').close();
    window.posts=[];
    window.creation=new NewTaskView({json:async(path,options)=>{
      if(options?.method==='POST') {
        window.posts.push(JSON.parse(options.body));
        if(window.holdCreation) await new Promise(resolve=>window.releaseCreation=resolve);
        if(window.creationSucceeds) return {id:'created'};
        throw new Error('fixture failure');
      }
      if(path==='/api/backends' && window.holdMetadata) await new Promise((resolve,reject)=>{window.releaseMetadata=resolve;window.rejectMetadata=()=>reject(new Error('metadata failure'));});
      return path==='/api/backends'?{backends}:path==='/api/accounts?project=alpha'||path==='/api/accounts?project=beta'?[]:path==='/api/settings'?{defaults:{backend:'coder'}}:path==='/api/projects/full'?{projects:[{name:'alpha',backend:'assistant'},{name:'beta'}]}:{};
    }},()=>{},()=>{});
    window.creation.open('alpha',()=>{});
  });
  await expect(page.locator('#new-task-backend option')).toHaveCount(4);
});
test('NewTaskView limits curated models and preserves explicit defaults',async({page})=>{
  const model=page.getByLabel('Model', {exact:true}),sandbox=page.getByLabel('Sandbox',{exact:true});
  await expect(model).toBeDisabled();
  expect(await model.locator('option').allTextContents()).toEqual(['Daemon default']);
  await expect(sandbox).toHaveValue('');
  expect(await sandbox.locator('option').allTextContents()).toEqual(['Project default','Enabled','Disabled']);
  await page.getByLabel('Prompt',{exact:true}).fill('Run the task');
  const submit=async()=>{await page.getByRole('button',{name:'Start task',exact:true}).click();await expect(page.getByRole('alert')).toContainText('fixture failure');};
  await submit();
  expect(await page.evaluate(()=>window.posts.at(-1))).toEqual({project:'alpha',prompt:'Run the task'});
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('assistant');
  expect(await model.locator('option').allTextContents()).toEqual(['Backend default (sonnet)','sonnet','opus']);
  await model.selectOption('sonnet');await sandbox.selectOption('enabled');await submit();
  expect(await page.evaluate(()=>window.posts.at(-1))).toEqual({project:'alpha',prompt:'Run the task',backend:'assistant',model:'sonnet',sandbox_override:'enabled'});
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('custom');
  await expect(model).toHaveValue('');await expect(model).toHaveAttribute('placeholder','Backend default (provider-default)');
  await model.fill(' custom-model ');await sandbox.selectOption('disabled');await submit();
  expect(await page.evaluate(()=>window.posts.at(-1))).toEqual({project:'alpha',prompt:'Run the task',backend:'custom',model:'custom-model',sandbox_override:'disabled'});
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('coder');
  await expect(model).toHaveValue('');
  expect(await model.locator('option').allTextContents()).toEqual(['Backend default (gpt-default)','gpt-custom']);
  await model.focus();await page.keyboard.press('Tab');await expect(sandbox).toBeFocused();
  await page.keyboard.press('Tab');await expect(page.getByLabel('Prompt',{exact:true})).toBeFocused();
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('');
  await expect(model).toBeDisabled();await expect(model).toHaveValue('');
  expect(await model.locator('option').allTextContents()).toEqual(['Daemon default']);
});
test('NewTaskView retains choices per project and clears them on disconnect',async({page})=>{
  const model=page.getByLabel('Model',{exact:true}),sandbox=page.getByLabel('Sandbox',{exact:true});
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('assistant');await model.selectOption('opus');await sandbox.selectOption('disabled');
  await page.evaluate(()=>window.creation.open('beta',()=>{}));
  await expect(model).toHaveValue('');await expect(sandbox).toHaveValue('');
  await expect(model).toBeDisabled();
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('coder');await model.selectOption('gpt-custom');await sandbox.selectOption('enabled');
  await page.evaluate(()=>window.creation.open('alpha',()=>{}));
  await expect(model).toHaveValue('opus');await expect(sandbox).toHaveValue('disabled');
  await page.getByLabel('Prompt',{exact:true}).fill('Keep failed draft');
  await page.getByRole('button',{name:'Start task',exact:true}).click();await expect(page.getByRole('alert')).toContainText('fixture failure');
  await page.keyboard.press('Escape');await page.evaluate(()=>window.creation.open('alpha',()=>{}));
  await expect(model).toHaveValue('opus');await expect(sandbox).toHaveValue('disabled');
  await page.evaluate(()=>{window.creation.clear();window.creation.open('alpha',()=>{});});
  await expect(model).toHaveValue('');await expect(sandbox).toHaveValue('');
});

test('NewTaskView disables choices during creation and clears successful drafts',async({page})=>{
  await page.setViewportSize({width:390,height:844});
  const model=page.getByLabel('Model',{exact:true}),sandbox=page.getByLabel('Sandbox',{exact:true});
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('assistant');await model.selectOption('opus');await sandbox.selectOption('enabled');
  await page.getByLabel('Prompt',{exact:true}).fill('Start successfully');
  await page.evaluate(()=>{window.holdCreation=true;window.creationSucceeds=true;});
  await page.getByRole('button',{name:'Start task',exact:true}).click();
  await expect(model).toBeDisabled();await expect(sandbox).toBeDisabled();
  await page.evaluate(()=>window.releaseCreation());
  await expect(page.locator('#new-task-view')).toBeHidden();
  expect(await page.evaluate(()=>window.posts.at(-1))).toEqual({project:'alpha',prompt:'Start successfully',backend:'assistant',model:'opus',sandbox_override:'enabled'});
  await page.evaluate(()=>window.creation.open('alpha',()=>{}));
  await expect(model).toHaveValue('');await expect(sandbox).toHaveValue('');
  await expect(model).toBeDisabled();await sandbox.focus();
  expect(await sandbox.evaluate(field=>getComputedStyle(field).outlineStyle)).toBe('solid');
  expect(await sandbox.evaluate(field=>field.getBoundingClientRect().right<=innerWidth)).toBe(true);
});

test('NewTaskView rejects an unlisted curated model before posting',async({page})=>{
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('assistant');
  await page.getByLabel('Model',{exact:true}).evaluate(field=>{
    const option=document.createElement('option');option.value='unlisted';field.append(option);field.value='unlisted';
  });
  await page.getByLabel('Prompt',{exact:true}).fill('Keep requested model');
  await page.getByRole('button',{name:'Start task',exact:true}).click();
  await expect(page.getByRole('alert')).toContainText('Choose a listed model');
  expect(await page.evaluate(()=>window.posts)).toEqual([]);
  await expect(page.getByLabel('Prompt',{exact:true})).toHaveValue('Keep requested model');
  await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeEnabled();
});

async function reopenSavedModel(page) {
  await page.getByLabel('Backend (optional)',{exact:true}).selectOption('assistant');
  await page.getByLabel('Model',{exact:true}).selectOption('opus');
  await page.getByLabel('Sandbox',{exact:true}).selectOption('disabled');
  await page.getByLabel('Prompt',{exact:true}).fill('Keep requested model');
  await page.getByRole('button',{name:'Cancel new task'}).click();
  await page.evaluate(()=>{window.holdMetadata=true;window.creation.open('alpha',()=>{});});
}

test('NewTaskView preserves a saved model when canceled before metadata resolves',async({page})=>{
  await reopenSavedModel(page);
  await page.getByRole('button',{name:'Cancel new task'}).click();
  await page.evaluate(()=>{window.holdMetadata=false;window.releaseMetadata();window.creation.open('alpha',()=>{});});
  await expect(page.getByLabel('Model',{exact:true})).toHaveValue('opus');
  await expect(page.getByLabel('Sandbox',{exact:true})).toHaveValue('disabled');
});

test('NewTaskView blocks saved-model submission until metadata resolves',async({page})=>{
  await reopenSavedModel(page);
  const start=page.getByRole('button',{name:'Start task',exact:true});
  await expect(start).toBeDisabled();
  await page.getByLabel('Prompt',{exact:true}).press('Enter');
  await page.locator('.new-task-form').evaluate(form=>form.requestSubmit());
  expect(await page.evaluate(()=>window.posts)).toEqual([]);
  await page.evaluate(()=>window.releaseMetadata());
  await expect(page.getByLabel('Model',{exact:true})).toHaveValue('opus');
  await expect(start).toBeEnabled();
  await start.click();
  await expect(page.getByRole('alert')).toContainText('fixture failure');
  expect(await page.evaluate(()=>window.posts)).toEqual([{project:'alpha',prompt:'Keep requested model',backend:'assistant',model:'opus',sandbox_override:'disabled'}]);
});

test('NewTaskView preserves a saved model through metadata failure and cancel',async({page})=>{
  await reopenSavedModel(page);
  await page.evaluate(()=>window.rejectMetadata());
  await expect.poll(()=>page.evaluate(()=>window.creation.defaultBackend)).toBe('assistant');
  await page.getByLabel('Prompt',{exact:true}).fill('Keep model after metadata failure');
  await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeDisabled();
  await page.getByRole('button',{name:'Cancel new task'}).click();
  await page.evaluate(()=>{window.holdMetadata=false;window.creation.open('alpha',()=>{});});
  await expect(page.getByLabel('Model',{exact:true})).toHaveValue('opus');
});

for (const backend of ['', 'coder']) {
  test(`NewTaskView honors backend edit ${backend || 'Daemon default'} during saved-model loading`,async({page})=>{
    await reopenSavedModel(page);
    await page.getByLabel('Backend (optional)',{exact:true}).selectOption(backend);
    await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeEnabled();
    await page.evaluate(()=>window.releaseMetadata());
    await expect.poll(()=>page.evaluate(()=>window.creation.backends.length)).toBe(3);
    await expect(page.getByLabel('Backend (optional)',{exact:true})).toHaveValue(backend);
    await expect(page.getByLabel('Model',{exact:true})).toHaveValue('');
    await page.getByRole('button',{name:'Cancel new task'}).click();
    await page.evaluate(()=>{window.holdMetadata=false;window.creation.open('alpha',()=>{});});
    await expect.poll(()=>page.evaluate(()=>window.creation.backends.length)).toBe(3);
    await expect(page.getByLabel('Backend (optional)',{exact:true})).toHaveValue(backend);
    await expect(page.getByLabel('Model',{exact:true})).toHaveValue('');
  });
}
