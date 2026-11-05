import { logger } from "@clerk/shared/logger";

//#region src/utils/cssLayerName.ts
const NON_ASCII_IDENT = "\\u00B7\\u00C0-\\u00D6\\u00D8-\\u00F6\\u00F8-\\u037D\\u037F-\\u1FFF\\u200C-\\u200D\\u203F-\\u2040\\u2070-\\u218F\\u2C00-\\u2FEF\\u3001-\\uD7FF\\uF900-\\uFDCF\\uFDF0-\\uFFFD\\u{10000}-\\u{10FFFF}";
const CSS_IDENT_RE = new RegExp(`^(?:--|-?${`[A-Za-z_${NON_ASCII_IDENT}]`})${`[A-Za-z0-9_\\-${NON_ASCII_IDENT}]`}*$`, "u");
const CSS_WIDE_KEYWORDS = new Set([
	"initial",
	"inherit",
	"unset",
	"revert",
	"revert-layer",
	"revert-rule"
]);
function isValidCssLayerName(value) {
	return typeof value === "string" && value.split(".").every((segment) => CSS_IDENT_RE.test(segment) && !CSS_WIDE_KEYWORDS.has(segment.toLowerCase()));
}
function sanitizeCssLayerName(value) {
	if (!value) return;
	if (isValidCssLayerName(value)) return value;
	logger.warnOnce(`Clerk: ignoring invalid \`cssLayerName\` ${JSON.stringify(value)}. It must be a CSS layer name such as "clerk" or "app.components".`);
}

//#endregion
export { sanitizeCssLayerName };
//# sourceMappingURL=cssLayerName.js.map