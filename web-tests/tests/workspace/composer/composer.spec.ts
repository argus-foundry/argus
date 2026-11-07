import { test } from '@playwright/test';
import { resetServer } from '../../_helpers';
// Browser text insertion and synthetic clipboard events avoid reading or changing the user's OS clipboard.
import assert from 'node:assert/strict';
test('composer composer', async ({
  browser
}) => {
  await resetServer();
  const frontend = process.env.WORKSPACE_TEST_URL || 'http://127.0.0.1:17750';
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const fixture = `<!doctype html><label>Search<input id="search"></label>
  <form id="composer"><label for="draft">Message</label><textarea id="draft"></textarea><button id="send">Send</button></form>
  <span id="composer-label"></span><span id="composer-status"></span>
  <dialog id="login"><input id="token" type="password"></dialog>
  <script type="module">
    import { TaskComposer } from '/composer/composer.js';
    const sent = [], taskA = {id:'a',name:'A'}, taskB = {id:'b',name:'B'};
    let resolve;
    const composer = new TaskComposer(async (text,task) => { sent.push({text,task:task.id}); if(window.fixture.fail) throw new Error('Fixture send failed'); if(window.fixture.block) await new Promise(done => {resolve=done;}); });
    window.fixture = {composer,sent,block:false,release:()=>resolve(),select:task=>{composer.select(task);composer.setInteractive(true);composer.focus();},taskA,taskB};
    window.fixture.select(taskA);
  </script>`;
  const count = () => page.evaluate(() => window.fixture.sent.length);
  {
    await page.route('**/composer-fixture', route => route.fulfill({
      contentType: 'text/html',
      body: fixture
    }));
    await page.goto(frontend + '/composer-fixture');
    await page.waitForFunction(() => window.fixture);
    assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    const paste = text => page.locator('#draft').evaluate((field, text) => {
      const clipboardData = new DataTransfer();
      clipboardData.setData('text/plain', text);
      return field.dispatchEvent(new ClipboardEvent('paste', {
        bubbles: true,
        cancelable: true,
        clipboardData
      }));
    }, text);
    await page.locator('#draft').fill('alpha omega');
    await page.locator('#draft').evaluate(field => field.setSelectionRange(6, 11));
    assert.equal(await paste('bravo'), false);
    assert.equal(await page.locator('#draft').inputValue(), 'alpha bravo');
    assert.equal(await page.locator('#draft').evaluate(field => field.selectionStart), 11);
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+z' : 'Control+z');
    assert.equal(await page.locator('#draft').inputValue(), 'alpha omega');
    assert.equal(await page.evaluate(() => window.fixture.composer.drafts.get('a')), 'alpha omega');
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+Shift+z' : 'Control+Shift+z');
    assert.equal(await page.locator('#draft').inputValue(), 'alpha bravo');
    await page.locator('#draft').fill('');
    for (const phrase of ['alpha', ' bravo charlie', '\nnext line\n']) await paste(phrase);
    assert.equal(await page.locator('#draft').inputValue(), 'alpha bravo charlie\nnext line\n');
    assert.equal(await count(), 0);
    const selection = await page.locator('#draft').evaluate(field => [field.selectionStart, field.selectionEnd]);
    assert.equal(await paste(''), true);
    assert.deepEqual(await page.locator('#draft').evaluate(field => [field.selectionStart, field.selectionEnd]), selection);
    await page.evaluate(() => window.fixture.select(window.fixture.taskB));
    await paste('B draft');
    await page.evaluate(() => window.fixture.select(window.fixture.taskA));
    assert.equal(await page.locator('#draft').inputValue(), 'alpha bravo charlie\nnext line\n');
    await page.locator('#draft').evaluate(field => {
      field.readOnly = true;
    });
    assert.equal(await paste('ignored'), true);
    await page.locator('#draft').evaluate(field => {
      field.readOnly = false;
    });
    for (const behavior of ['unsupported', 'throws', 'inserts-but-false', 'silent-success']) {
      await page.locator('#draft').fill('start end');
      await page.locator('#draft').evaluate(field => field.setSelectionRange(6, 9));
      await page.evaluate(behavior => {
        window.originalExec = document.execCommand;
        document.execCommand = (...args) => {
          if (behavior === 'throws') throw new Error('Unsupported');
          if (behavior === 'inserts-but-false') window.originalExec.apply(document, args);
          return behavior === 'silent-success';
        };
      }, behavior);
      await paste('replacement');
      await page.evaluate(() => {
        document.execCommand = window.originalExec;
      });
      assert.equal(await page.locator('#draft').inputValue(), 'start replacement');
      assert.deepEqual(await page.locator('#draft').evaluate(field => [field.selectionStart, field.selectionEnd]), [17, 17]);
      assert.equal(await page.evaluate(() => window.fixture.composer.drafts.get('a')), 'start replacement');
    }
    await page.locator('#draft').fill('');
    console.log('PASS focused repeated paste, selection, undo/redo, task drafts, empty/read-only input and compatibility fallback');
    const dictation = 'Dictated first line\nSecond line\n';
    await page.keyboard.insertText(dictation);
    assert.equal(await page.locator('#draft').inputValue(), dictation);
    assert.equal(await count(), 0);
    await page.locator('#draft').fill('First');
    await page.keyboard.press('Shift+Enter');
    await page.keyboard.insertText('Second');
    assert.equal(await page.locator('#draft').inputValue(), 'First\nSecond');
    assert.equal(await count(), 0);
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => document.querySelector('#draft').value === '');
    assert.deepEqual(await page.evaluate(() => window.fixture.sent[0]), {
      text: 'First\nSecond',
      task: 'a'
    });
    assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    console.log('PASS text insertion and Shift+Enter do not send; ordinary Enter sends once and restores focus');
    await page.locator('#draft').fill('Composing');
    await page.locator('#draft').evaluate(field => {
      field.dispatchEvent(new CompositionEvent('compositionstart', {
        bubbles: true
      }));
      field.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'Enter',
        bubbles: true,
        cancelable: true
      }));
      field.dispatchEvent(new CompositionEvent('compositionend', {
        bubbles: true
      }));
      for (const extra of [{
        isComposing: true
      }, {
        keyCode: 229
      }, {
        repeat: true
      }]) {
        field.dispatchEvent(new KeyboardEvent('keydown', {
          key: 'Enter',
          bubbles: true,
          cancelable: true,
          ...extra
        }));
      }
    });
    assert.equal(await count(), 1);
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => window.fixture.sent.length === 2);
    console.log('PASS IME confirmation, keyCode 229 and repeated Enter cannot submit');
    const fallback = 'Pasted while no field was active\n';
    await page.evaluate(text => {
      document.body.tabIndex = -1;
      document.body.focus();
      const clipboardData = new DataTransfer();
      clipboardData.setData('text/plain', text);
      document.dispatchEvent(new ClipboardEvent('paste', {
        bubbles: true,
        cancelable: true,
        clipboardData
      }));
    }, fallback);
    assert.equal(await page.locator('#draft').inputValue(), fallback);
    assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    assert.equal(await count(), 2);
    await page.locator('#search').focus();
    const allowed = await page.locator('#search').evaluate(field => {
      const clipboardData = new DataTransfer();
      clipboardData.setData('text/plain', 'search text');
      return field.dispatchEvent(new ClipboardEvent('paste', {
        bubbles: true,
        cancelable: true,
        clipboardData
      }));
    });
    assert.equal(allowed, true);
    await page.keyboard.insertText('search text');
    assert.equal(await page.locator('#search').inputValue(), 'search text');
    assert.equal(await page.locator('#draft').inputValue(), fallback);
    await page.evaluate(() => window.fixture.composer.setInteractive(true));
    assert.equal(await page.locator('#search').evaluate(field => document.activeElement === field), true);
    console.log('PASS background paste targets composer; editable fields and status updates keep their focus');
    await page.evaluate(() => document.querySelector('#login').showModal());
    await page.locator('#token').focus();
    const tokenPasteAllowed = await page.locator('#token').evaluate(field => {
      const clipboardData = new DataTransfer();
      clipboardData.setData('text/plain', 'fixture-credential');
      return field.dispatchEvent(new ClipboardEvent('paste', {
        bubbles: true,
        cancelable: true,
        clipboardData
      }));
    });
    assert.equal(tokenPasteAllowed, true);
    await page.keyboard.insertText('fixture-credential');
    assert.equal(await page.locator('#draft').inputValue(), fallback);
    await page.evaluate(() => document.querySelector('#login').close());
    const protectedDraft = await page.locator('#draft').inputValue();
    await page.evaluate(() => {
      const paste = target => {
        const clipboardData = new DataTransfer();
        clipboardData.setData('text/plain', 'do not redirect');
        const event = new ClipboardEvent('paste', {
          bubbles: true,
          cancelable: true,
          clipboardData
        });
        target.dispatchEvent(event);
        return event.defaultPrevented;
      };
      for (const markup of ['<div class="xterm" tabindex="0"></div>', '<div contenteditable="true" tabindex="0"></div>']) {
        const host = document.createElement('div');
        host.innerHTML = markup;
        document.body.append(host);
        const target = host.firstChild;
        target.focus();
        if (paste(target)) throw new Error('Protected paste was intercepted');
        host.remove();
      }
      document.querySelector('#composer').inert = true;
      document.body.tabIndex = -1;
      document.body.focus();
      if (paste(document.body)) throw new Error('Inert composer intercepted paste');
      document.querySelector('#composer').inert = false;
      const clipboardData = new DataTransfer();
      clipboardData.setData('text/plain', 'already handled');
      const event = new ClipboardEvent('paste', {
        bubbles: true,
        cancelable: true,
        clipboardData
      });
      event.preventDefault();
      document.dispatchEvent(event);
    });
    assert.equal(await page.locator('#draft').inputValue(), protectedDraft);
    console.log('PASS terminal, editable, dialog, inert and already handled paste are not redirected');
    await page.locator('#draft').fill('Click send');
    await page.locator('#send').click();
    await page.waitForFunction(() => document.querySelector('#draft').value === '');
    assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    console.log('PASS login keeps its input and clicking Send returns focus to the composer');
    await page.evaluate(() => {
      window.fixture.block = true;
    });
    await page.locator('#draft').fill('Pending for A');
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => document.querySelector('#send').textContent.includes('Sending'));
    assert.equal(await page.locator('#send').getAttribute('data-tooltip'), 'Sending message');
    await page.evaluate(() => window.fixture.select(window.fixture.taskB));
    await page.locator('#draft').fill('B stays here');
    await page.locator('#search').focus();
    await page.evaluate(() => window.fixture.release());
    await page.waitForFunction(() => !window.fixture.composer.pending.has('a'));
    assert.equal(await page.locator('#draft').inputValue(), 'B stays here');
    assert.equal(await page.locator('#search').evaluate(field => document.activeElement === field), true);
    assert.deepEqual(await page.evaluate(() => window.fixture.sent.at(-1)), {
      text: 'Pending for A',
      task: 'a'
    });
    console.log('PASS pending completion cannot clear another task draft or steal focus');
    await page.locator('#draft').focus();
    await page.locator('#draft').fill('Submitted draft');
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => window.fixture.composer.pending.has('b'));
    await page.locator('#draft').fill('New ');
    await paste('text typed while sending');
    await page.evaluate(() => window.fixture.release());
    await page.waitForFunction(() => !window.fixture.composer.pending.has('b'));
    assert.equal(await page.locator('#draft').inputValue(), 'New text typed while sending');
    await page.evaluate(() => {
      window.fixture.block = false;
      window.fixture.fail = true;
    });
    await page.locator('#draft').fill('Keep this failed draft');
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => document.querySelector('#composer-status').textContent.includes('Fixture send failed'));
    assert.equal(await page.locator('#draft').inputValue(), 'Keep this failed draft');
    assert.equal(await page.locator('#send').isEnabled(), true);
    assert.deepEqual(errors, []);
    console.log('PASS new edits survive successful delivery and failed sends retain an eligible draft');
    await page.evaluate(() => {
      window.fixture.block = false;
      window.fixture.fail = false;
      window.fixture.select(window.fixture.taskA);
    });
    for (let index = 0; index < 5; index++) {
      await paste('Replace this phrase');
      await page.keyboard.press(process.platform === 'darwin' ? 'Meta+a' : 'Control+a');
      await page.keyboard.press('Backspace');
      await paste(`Repeated phrase ${index}`);
      await page.keyboard.press('Enter');
      await page.waitForFunction(() => document.querySelector('#draft').value === '');
      assert.equal(await page.locator('#draft').evaluate(field => document.activeElement === field), true);
    }
    const mutations = await page.evaluate(() => {
      const observer = new MutationObserver(() => {});
      observer.observe(document.body, {
        childList: true,
        attributes: true,
        subtree: true,
        characterData: true
      });
      for (let index = 0; index < 20; index++) window.fixture.composer.render();
      const count = observer.takeRecords().length;
      observer.disconnect();
      return count;
    });
    assert.equal(mutations, 0);
    assert.deepEqual(await page.evaluate(() => window.fixture.sent.slice(-5).map(message => message.text)), Array.from({
      length: 5
    }, (_, index) => `Repeated phrase ${index}`));
    console.log('PASS repeated insert, erase and submit cycles; unchanged rendering causes zero DOM mutations');
  }
});
test.afterEach(async ({
  browser
}) => {
  await Promise.all(browser.contexts().map(context => context.close()));
});
