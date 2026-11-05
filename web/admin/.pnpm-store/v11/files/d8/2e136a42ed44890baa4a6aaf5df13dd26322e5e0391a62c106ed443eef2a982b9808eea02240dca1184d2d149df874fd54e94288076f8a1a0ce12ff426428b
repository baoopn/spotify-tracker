import { shouldHandOffToEnterpriseConnection, shouldHandOffUnidentifiedToEnterpriseConnection } from "./enterpriseSSOFactors.js";
import { isClerkRuntimeError } from "@clerk/shared/error";
import { ERROR_CODES } from "@clerk/shared/internal/clerk-js/constants";

//#region src/components/SignIn/handleProtectCheck.ts
/**
* Detects whether a sign-in response is gated by Clerk Protect.
*
* The `protectCheck` field is the authoritative gating signal; new SDKs / newer servers
* also surface `status === 'needs_protect_check'`. Either signal triggers navigation
* to the protect-check route.
*/
function isSignInProtectGated(signIn) {
	return !!signIn.protectCheck || signIn.status === "needs_protect_check";
}
/**
* Single choke point for routing a Clerk Protect gate during sign-in.
*
* Every sign-in operation that returns a `SignInResource` (create, attempt/prepare first/second
* factor, passkey, reset-password, web3, …) can be gated mid-flow, and a missed call site strands
* the user at the previous step. Funnel them all through this helper: call it right after the
* operation resolves and `return` when it returns `true`, before dispatching on `signIn.status`.
*
* The `protectCheckPath` is supplied per call site because the prebuilt UI mounts sign-in steps at
* different route depths — `SignInStart` (index) reaches the route at `'protect-check'`, the factor
* cards reach it at `'../protect-check'`.
*
* @returns `true` if the response was gated and navigation was issued (caller should stop).
*/
function navigateOnSignInProtectGate(signIn, navigate, protectCheckPath) {
	if (isSignInProtectGated(signIn)) {
		navigate(protectCheckPath);
		return true;
	}
	return false;
}
/**
* Whether `err` is the error `authenticateWithRedirect` throws when a challenge stopped it before it
* could redirect. The sign-in has already been updated and is sitting on the gate, so the caller
* routes to the challenge rather than showing the error.
*/
function isProtectCheckRequiredError(err) {
	if (typeof err !== "object" || err === null) return false;
	return isClerkRuntimeError(err) && err.code === ERROR_CODES.PROTECT_CHECK_REQUIRED;
}
/**
* Whether this sign-in is waiting to become a sign-up.
*/
function isSignInPendingOAuthTransfer(signIn) {
	return signIn.firstFactorVerification?.status === "transferable";
}
function resumeSignInAfterProtectCheck(signIn, { navigate, resumeEnterpriseSSO, resumeOAuthContinuation, startedAsOAuthTransfer }) {
	if (isSignInProtectGated(signIn)) return navigate(".");
	switch (signIn.status) {
		case "needs_first_factor":
			if (shouldHandOffToEnterpriseConnection(signIn)) return resumeEnterpriseSSO();
			return navigate("../factor-one");
		case "needs_second_factor": return navigate("../factor-two");
		case "needs_client_trust": return navigate("../client-trust");
		case "needs_new_password": return navigate("../reset-password");
		case "needs_identifier":
			if (startedAsOAuthTransfer || isSignInPendingOAuthTransfer(signIn)) return resumeOAuthContinuation();
			if (shouldHandOffUnidentifiedToEnterpriseConnection(signIn)) return resumeEnterpriseSSO();
			return navigate("..");
		default: return startedAsOAuthTransfer || isSignInPendingOAuthTransfer(signIn) ? resumeOAuthContinuation() : navigate("..");
	}
}

//#endregion
export { isProtectCheckRequiredError, isSignInPendingOAuthTransfer, isSignInProtectGated, navigateOnSignInProtectGate, resumeSignInAfterProtectCheck };
//# sourceMappingURL=handleProtectCheck.js.map