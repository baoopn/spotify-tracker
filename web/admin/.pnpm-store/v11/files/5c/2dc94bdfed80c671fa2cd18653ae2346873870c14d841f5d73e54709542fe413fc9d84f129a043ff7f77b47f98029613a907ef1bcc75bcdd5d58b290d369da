import { useRouter } from "../../router/RouteContext.js";
import { useNavigateToFlowStart } from "../../hooks/useNavigateToFlowStart.js";
import { useSignInContext } from "../../contexts/components/SignIn.js";
import { useCoreSignIn } from "../../contexts/CoreClientContext.js";
import { useCardState, withCardStateProvider } from "../../elements/contexts/index.js";
import { actionBlockedDetailsFrom } from "../../utils/actionBlocked.js";
import { ActionBlockedCard } from "../../common/ActionBlockedCard.js";
import { withRedirectToAfterSignIn } from "../../common/withRedirect.js";
import { useProtectCheckRunner } from "../../hooks/useProtectCheckRunner.js";
import { ProtectCheckCard } from "../ProtectCheck/ProtectCheckCard.js";
import { buildSignInOAuthCallbackParams } from "./buildOAuthCallbackParams.js";
import { isProtectCheckRequiredError, isSignInPendingOAuthTransfer, isSignInProtectGated, resumeSignInAfterProtectCheck } from "./handleProtectCheck.js";
import { useEffect, useRef, useState } from "react";
import { removeClerkQueryParam } from "@clerk/shared/internal/clerk-js/queryParams";
import { useClerk } from "@clerk/shared/react";
import { jsx } from "@emotion/react/jsx-runtime";

//#region src/components/SignIn/SignInProtectCheck.tsx
function SignInProtectCheckInternal() {
	const card = useCardState();
	const signIn = useCoreSignIn();
	const { navigate } = useRouter();
	const { navigateToFlowStart } = useNavigateToFlowStart();
	const { setActive, __internal_resumeAfterProtectCheck } = useClerk();
	const ctx = useSignInContext();
	const { afterSignInUrl, navigateOnSetActive } = ctx;
	const startedAsOAuthTransfer = useRef(isSignInPendingOAuthTransfer(signIn));
	const [everSawProtectCheck, setEverSawProtectCheck] = useState(!!signIn.protectCheck);
	const didStartNoCheckFallbackRef = useRef(false);
	if (signIn.protectCheck && !everSawProtectCheck) setEverSawProtectCheck(true);
	useEffect(() => {
		if (!signIn.protectCheck && !everSawProtectCheck && !didStartNoCheckFallbackRef.current) {
			didStartNoCheckFallbackRef.current = true;
			navigateToFlowStart();
		}
	}, [
		everSawProtectCheck,
		navigateToFlowStart,
		signIn.protectCheck
	]);
	const runner = useProtectCheckRunner({
		getProtectCheck: () => signIn.protectCheck,
		getResource: () => signIn,
		reload: () => signIn.reload(),
		submitProtectCheck: (params) => signIn.submitProtectCheck(params),
		onResolved: async (updatedSignIn, isCancelled) => {
			if (isCancelled()) return;
			if (updatedSignIn.status === "complete" && updatedSignIn.createdSessionId) {
				removeClerkQueryParam("__clerk_ticket");
				await setActive({
					session: updatedSignIn.createdSessionId,
					navigate: async ({ session, decorateUrl }) => {
						await navigateOnSetActive({
							session,
							redirectUrl: afterSignInUrl,
							decorateUrl
						});
					}
				});
				return;
			}
			await resumeSignInAfterProtectCheck(updatedSignIn, {
				navigate,
				resumeEnterpriseSSO: async () => {
					try {
						await signIn.authenticateWithRedirect({
							strategy: "enterprise_sso",
							redirectUrl: ctx.ssoCallbackUrl,
							redirectUrlComplete: afterSignInUrl || "/",
							oidcPrompt: ctx.oidcPrompt,
							continueSignIn: true
						});
					} catch (err) {
						if (isProtectCheckRequiredError(err) && isSignInProtectGated(signIn)) {
							await navigate(".");
							return;
						}
						throw err;
					}
				},
				startedAsOAuthTransfer: startedAsOAuthTransfer.current,
				resumeOAuthContinuation: () => typeof __internal_resumeAfterProtectCheck === "function" ? __internal_resumeAfterProtectCheck({
					...buildSignInOAuthCallbackParams(ctx),
					continuation: "transfer_to_sign_up",
					__internal_navigateOnSetActive: ctx.navigateOnSetActive
				}, navigate) : navigate("..")
			});
		}
	});
	if (!signIn.protectCheck && !everSawProtectCheck) return null;
	const blockedDetails = actionBlockedDetailsFrom(card.rawError);
	if (blockedDetails) return /* @__PURE__ */ jsx(ActionBlockedCard, { details: blockedDetails });
	return /* @__PURE__ */ jsx(ProtectCheckCard, {
		flow: "signIn",
		runner
	});
}
const SignInProtectCheck = withRedirectToAfterSignIn(withCardStateProvider(SignInProtectCheckInternal));

//#endregion
export { SignInProtectCheck };
//# sourceMappingURL=SignInProtectCheck.js.map