import { clerkCoreErrorContextProviderNotFound } from "@clerk/shared/internal/clerk-js/errors";
import { populateParamFromObject } from "@clerk/shared/url";
import { snakeToCamel } from "@clerk/shared/underscore";

//#region src/contexts/utils.ts
function assertContextExists(contextVal, providerName) {
	if (!contextVal) clerkCoreErrorContextProviderNotFound(providerName);
}
function getInitialValuesFromQueryParams(queryString, params) {
	const props = {};
	new URLSearchParams(queryString).forEach((value, key) => {
		if (params.includes(key) && typeof value === "string") props[snakeToCamel(key)] = value;
	});
	return props;
}

//#endregion
export { assertContextExists, getInitialValuesFromQueryParams, populateParamFromObject };
//# sourceMappingURL=utils.js.map