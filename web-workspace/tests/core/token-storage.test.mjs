import test from 'node:test';
import assert from 'node:assert/strict';
import { TokenStorage, TOKEN_KEY } from '../../core/token-storage.js';

function fixture() {
  const values = new Map();
  const storage = { getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) };
  return { values, tokens: new TokenStorage(() => storage) };
}

test('only a successful remembered connection stores the token under the workspace key', () => {
  const { values, tokens } = fixture();
  assert.equal(tokens.read(), '');
  tokens.rejected(new Error('network'));
  assert.equal(values.size, 0);
  tokens.connected('test-token', true);
  assert.deepEqual([...values], [[TOKEN_KEY, 'test-token']]);
  assert.equal(tokens.read(), 'test-token');
});

for (const status of [401, 403]) {
  test(`authentication rejection ${status} clears the saved token`, () => {
    const { tokens } = fixture();
    tokens.connected('test-token', true);
    assert.equal(tokens.rejected({ status }), true);
    assert.equal(tokens.read(), '');
  });
}
for (const error of [{ status: 500 }, { status: 503 }, new Error('network'), { name: 'AbortError' }]) {
  test(`temporary failure ${error.status || error.name} retains the saved token`, () => {
    const { tokens } = fixture();
    tokens.connected('test-token', true);
    assert.equal(tokens.rejected(error), false);
    assert.equal(tokens.read(), 'test-token');
  });
}

test('unchecked Remember and explicit disconnect remove only the workspace token', () => {
  const { values, tokens } = fixture();
  values.set('argus-token', 'original-ui-token');
  tokens.connected('test-token', true);
  tokens.connected('session-token', false);
  assert.equal(tokens.read(), '');
  tokens.connected('test-token', true);
  tokens.clear();
  assert.deepEqual([...values], [['argus-token', 'original-ui-token']]);
});

for (const operation of ['access', 'getItem', 'setItem', 'removeItem']) {
  test(`blocked storage at ${operation} falls back without throwing`, () => {
    const tokens = new TokenStorage(() => {
      if (operation === 'access') throw new Error('blocked');
      return Object.fromEntries(['getItem', 'setItem', 'removeItem'].map(name => [name, () => {
        if (operation === name) throw new Error('blocked');
      }]));
    });
    assert.equal(tokens.read(), '');
    assert.doesNotThrow(() => tokens.connected('test-token', true));
    assert.doesNotThrow(() => tokens.connected('test-token', false));
    assert.doesNotThrow(() => tokens.rejected({ status: 401 }));
    assert.doesNotThrow(() => tokens.clear());
  });
}
