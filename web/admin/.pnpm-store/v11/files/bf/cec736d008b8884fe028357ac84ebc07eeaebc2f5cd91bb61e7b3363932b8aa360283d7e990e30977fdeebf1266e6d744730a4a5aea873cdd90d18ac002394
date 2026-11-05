//#region src/components/OAuthDeviceVerification/utils.ts
const USER_CODE_PATTERN = /^[BCDFGHJKLMNPQRSTVWXZ]{8}$/;
const USER_CODE_SEPARATOR_PATTERN = /[-\p{White_Space}]/gu;
const canReadLocation = () => typeof window !== "undefined" && !!window.location;
function normalizeOAuthDeviceUserCode(value) {
	return value.toUpperCase().replace(USER_CODE_SEPARATOR_PATTERN, "");
}
function isValidOAuthDeviceUserCode(value) {
	return USER_CODE_PATTERN.test(normalizeOAuthDeviceUserCode(value));
}
function getOAuthDeviceUserCodeFromSearch() {
	if (!canReadLocation()) return "";
	return new URLSearchParams(window.location.search).get("user_code") ?? "";
}

//#endregion
export { getOAuthDeviceUserCodeFromSearch, isValidOAuthDeviceUserCode, normalizeOAuthDeviceUserCode };
//# sourceMappingURL=utils.js.map