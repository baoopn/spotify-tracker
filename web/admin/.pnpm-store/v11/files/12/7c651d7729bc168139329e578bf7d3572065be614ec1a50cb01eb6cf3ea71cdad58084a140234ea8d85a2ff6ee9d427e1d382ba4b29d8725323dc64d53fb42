import type { AST } from 'svelte-eslint-parser';
import type { RuleContext } from '../../types.js';
/**
 * Runtime warnings that Svelte v5 lets you silence with `svelte-ignore`.
 * The compiler never reports them, so we can't tell whether the comment is used.
 * Keep in sync with `IGNORABLE_RUNTIME_WARNINGS` in the Svelte compiler:
 * https://github.com/sveltejs/svelte/blob/70afafe18e48a9973c5f711d62d87d088234655e/packages/svelte/src/constants.js#L47-L56
 */
export declare const RUNTIME_WARNING_CODES: Set<string>;
export type IgnoreItemWithoutCode = {
    range: [number, number];
    code: null;
    token: AST.Token | AST.Comment;
};
export type IgnoreItem = {
    range: [number, number];
    code: string;
    codeForV5: string;
    /**
     * Whether Svelte v5 reads this code in runes mode.
     * In runes mode, codes must be separated by commas, and everything after is prose.
     */
    readInRunesMode: boolean;
    token: AST.Token | AST.Comment;
};
/** Extract all svelte-ignore comment items */
export declare function getSvelteIgnoreItems(context: RuleContext): (IgnoreItem | IgnoreItemWithoutCode)[];
