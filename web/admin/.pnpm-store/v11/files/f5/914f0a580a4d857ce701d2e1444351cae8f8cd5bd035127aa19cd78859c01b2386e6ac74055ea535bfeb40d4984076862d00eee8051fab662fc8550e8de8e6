import { ERROR_CODES } from "@clerk/shared/internal/clerk-js/constants";

//#region src/utils/actionBlocked.ts
const getActionBlockedDetails = (error) => {
	const meta = error?.meta;
	if (!meta) return null;
	const { traceId, kind, title, description, linkUrl, linkText, data } = meta;
	if (!traceId && !title && !description && !linkUrl) return null;
	return {
		traceId,
		kind,
		title,
		description,
		linkUrl,
		linkText,
		data
	};
};
const actionBlockedDetailsFrom = (error) => {
	if (!error || typeof error !== "object") return null;
	if (error.code !== ERROR_CODES.FRAUD_ACTION_BLOCKED) return null;
	return getActionBlockedDetails(error);
};
const safeHref = (url) => {
	if (!url) return null;
	try {
		return new URL(url).protocol === "https:" ? url : null;
	} catch {
		return null;
	}
};

//#endregion
export { actionBlockedDetailsFrom, safeHref };
//# sourceMappingURL=actionBlocked.js.map