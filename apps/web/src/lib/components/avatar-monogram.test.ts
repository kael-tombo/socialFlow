import { describe, expect, it } from 'vitest';
import { monogramFor } from '@openpost/ui/components/avatar/monogram.js';

// Shared monogram rule (issue #400): the web account avatar and the native
// adapter must agree with this table. The preview package and the Go
// text-card renderer mirror the same rule in their own owners.
describe('shared account monogram', () => {
	it.each<[string, string | undefined, string]>([
		['Rodrigo Garcia', undefined, 'RG'],
		['Rodrigo', undefined, 'RO'],
		['SocialFlow.ai', undefined, 'OP'],
		['@ada', undefined, 'AD'],
		['  Ada   Lovelace  ', undefined, 'AL'],
		['x', undefined, 'X'],
		['', undefined, '?'],
		['   ', undefined, '?'],
		['@@@', undefined, '?'],
		['', '@ada', 'AD'],
		['   ', 'rodrigo', 'RO'],
		['Rodrigo Garcia', '@other', 'RG']
	])('monogramFor(%j, %j) is %j', (displayName, handle, expected) => {
		expect(monogramFor(displayName, handle)).toBe(expected);
	});
});
