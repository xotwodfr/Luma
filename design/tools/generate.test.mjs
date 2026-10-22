import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generate } from './generate.mjs';

const tokens = JSON.parse(readFileSync(new URL('../tokens.json', import.meta.url)));
const files = ['luma-tokens.css', 'LumaTokens.swift', 'LumaTokens.kt', 'LumaTokens.xaml', 'LumaTokens.qml', 'qmldir'];

test('emits every platform file with accent values, spacing and header', () => {
  const dir = mkdtempSync(join(tmpdir(), 'luma-gen-'));
  generate(tokens, dir);
  const strip = (hex) => hex.slice(1).toUpperCase();
  for (const f of files.filter((f) => f !== 'qmldir')) {
    const src = readFileSync(join(dir, f), 'utf8');
    assert.match(src, /Generated from design\/tokens\.json — do not edit\./, f);
    assert.ok(src.toUpperCase().includes(strip(tokens.color.light.lamp)), `${f} light lamp`);
    assert.ok(src.toUpperCase().includes(strip(tokens.color.dark.lamp)), `${f} dark lamp`);
    assert.match(src, /[^0-9.]16[^0-9.]/, `${f} space 16`);
  }
  assert.match(readFileSync(join(dir, 'qmldir'), 'utf8'), /singleton LumaTokens/);
});

test('output is deterministic', () => {
  const a = mkdtempSync(join(tmpdir(), 'luma-gen-'));
  const b = mkdtempSync(join(tmpdir(), 'luma-gen-'));
  generate(tokens, a);
  generate(tokens, b);
  for (const f of files) assert.equal(readFileSync(join(a, f), 'utf8'), readFileSync(join(b, f), 'utf8'), f);
});
