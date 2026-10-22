import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { ratio, checkAll } from './contrast.mjs';

const tokens = JSON.parse(readFileSync(new URL('../tokens.json', import.meta.url)));

test('ratio matches WCAG reference values', () => {
  assert.ok(Math.abs(ratio('#000000', '#ffffff') - 21) < 0.01);
  assert.ok(Math.abs(ratio('#777777', '#ffffff') - 4.48) < 0.01);
  assert.equal(ratio('#123456', '#123456'), 1);
});

test('every declared token pair meets its minimum in both themes', () => {
  const results = checkAll(tokens);
  assert.ok(results.length >= tokens.contrastPairs.length * 2);
  const failing = results.filter((r) => !r.ok);
  assert.deepEqual(failing, []);
});

test('every theme defines the same color names', () => {
  assert.deepEqual(Object.keys(tokens.color.light).sort(), Object.keys(tokens.color.dark).sort());
});
