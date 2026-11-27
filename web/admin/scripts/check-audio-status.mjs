// Listen-along audio status helper checks, run by `pnpm check` (node strips the TS types).
import assert from 'node:assert/strict';
import { STATUS_MAX_IDS, statusIds } from '../src/lib/audioStatus.ts';

const A = '4uLU6hMCjMI75M1A2tKUQC';
const B = '7ouMYWpwJ422jRcDASZB7P';

assert.deepEqual(statusIds([A, B]), [A, B], 'order kept, current song first');
assert.deepEqual(statusIds([A, B, A, B]), [A, B], 'no repeats');
assert.deepEqual(statusIds([null, undefined, '', A]), [A], 'empty ids skipped');
assert.deepEqual(statusIds(['spotify:local:x', 'short', A]), [A], 'only Spotify ids');
assert.deepEqual(statusIds([]), []);
const many = Array.from({ length: 80 }, (_, i) => `${String(i).padStart(2, '0')}${'x'.repeat(20)}`);
assert.equal(statusIds(many).length, STATUS_MAX_IDS, 'at most 50');
assert.equal(statusIds(many)[0], many[0]);

console.log('audio status checks: ok');
