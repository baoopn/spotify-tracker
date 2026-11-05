import { compilerVersion, svelteVersion } from "./svelte-version.js";
import { traverseNodes } from "../traverse.js";
const runeSymbols = [
    "$state",
    "$derived",
    "$effect",
    "$props",
    "$bindable",
    "$inspect",
    "$host",
];
export function resolveSvelteParseContextForSvelte(svelteConfig, parserOptions, svelteAst) {
    return {
        runes: isRunesAsParseContext(svelteConfig, parserOptions, svelteAst),
        compilerVersion,
        svelteConfig,
    };
}
export function resolveSvelteParseContextForSvelteScript(svelteConfig) {
    return {
        // .svelte.js files are always in Runes mode for Svelte 5.
        runes: svelteVersion.gte(5),
        compilerVersion,
        svelteConfig,
    };
}
function isRunesAsParseContext(svelteConfig, parserOptions, svelteAst) {
    // Svelte 3/4 does not support Runes mode.
    if (!svelteVersion.gte(5)) {
        return false;
    }
    // Parser option.
    if (parserOptions.svelteFeatures?.runes != null) {
        return parserOptions.svelteFeatures?.runes;
    }
    // `<svelte:options>` takes precedence over the compiler option, like the Svelte compiler.
    const svelteOptions = svelteAst.options;
    if (svelteOptions?.runes != null) {
        return svelteOptions?.runes;
    }
    // Compiler option.
    if (svelteConfig?.compilerOptions?.runes != null) {
        return typeof svelteConfig.compilerOptions?.runes === "function"
            ? svelteConfig.compilerOptions.runes({
                filename: parserOptions.filePath ?? "",
            })
            : svelteConfig.compilerOptions?.runes;
    }
    return undefined;
}
export function hasRunesSymbol(ast) {
    let hasRuneSymbol = false;
    traverseNodes(ast, {
        enterNode(node) {
            if (hasRuneSymbol) {
                return;
            }
            if (node.type === "Identifier" && runeSymbols.includes(node.name)) {
                hasRuneSymbol = true;
            }
        },
        leaveNode() {
            // do nothing
        },
    });
    return hasRuneSymbol;
}
