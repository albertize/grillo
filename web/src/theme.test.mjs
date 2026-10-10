// SPDX-License-Identifier: Apache-2.0
import test from 'node:test';
import assert from 'node:assert/strict';
import { THEME_KEY, themePreference, effectiveTheme, readTheme, saveTheme, applyTheme } from './theme.mjs';
test('theme defaults to browser preference; explicit choices override it', () => {
  assert.equal(effectiveTheme('system', true), 'dark');
  assert.equal(effectiveTheme('system', false), 'light');
  assert.equal(effectiveTheme('light', true), 'light');
  assert.equal(effectiveTheme('dark', false), 'dark');
  for (const value of [null, undefined, 'invalid', '<script>', {}, 'DARK']) {
    assert.equal(themePreference(value), 'system'); assert.equal(effectiveTheme(value, true), 'dark');
  }
});
test('theme persistence stores only allowed values; denied storage degrades safely', () => {
  const values = new Map(), storage = { getItem: key => values.get(key), setItem: (key, value) => values.set(key, value) };
  assert.equal(readTheme(storage), 'system');
  assert.equal(saveTheme(storage, 'dark'), true); assert.equal(values.get(THEME_KEY), 'dark'); assert.equal(readTheme(() => storage), 'dark');
  values.set(THEME_KEY, 'unknown'); assert.equal(readTheme(storage), 'system');
  assert.equal(saveTheme(storage, 'unknown'), true); assert.equal(values.get(THEME_KEY), 'system');
  const denied = () => { throw new Error('storage blocked'); };
  assert.equal(readTheme(denied), 'system'); assert.equal(saveTheme(denied, 'dark'), false);
});
test('theme applies PatternFly root class, effective mode and browser color metadata', () => {
  const toggles = [], attributes = [], document = { documentElement: { classList: { toggle: (...args) => toggles.push(args) }, dataset: {} }, querySelector: () => ({ setAttribute: (...args) => attributes.push(args) }) };
  assert.equal(applyTheme(document, 'system', true), 'dark');
  assert.deepEqual(toggles.at(-1), ['pf-v6-theme-dark', true]); assert.equal(document.documentElement.dataset.themePreference, 'system');
  assert.deepEqual(attributes.at(-1), ['content', '#171f25']);
  assert.equal(applyTheme(document, 'light', true), 'light'); assert.deepEqual(toggles.at(-1), ['pf-v6-theme-dark', false]);
});
