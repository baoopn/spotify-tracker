import { useEnvironment } from "../contexts/EnvironmentContext.js";
import { useCardState } from "../elements/contexts/index.js";
import { handleError } from "../utils/errorHandler.js";
import { ClerkRuntimeError } from "@clerk/shared/error";
import React from "react";
import { ERROR_CODES } from "@clerk/shared/internal/clerk-js/constants";
import { useClerk } from "@clerk/shared/react";
import { flushSync } from "react-dom";

//#region src/hooks/useProtectCheckRunner.ts
/**
* Shared driver for the `<SignInProtectCheck />` and `<SignUpProtectCheck />` cards. The challenge
* lifecycle itself lives in `runProtectCheck` from `@clerk/shared`. This hook binds it to the
* card's spinner, error, and continuation.
*
* Must be called from within a `CardStateProvider`.
*/
function useProtectCheckRunner(params) {
	const card = useCardState();
	useClerk().__internal_protectChallengeLoadTimeoutMs;
	useEnvironment().protectConfig?.challenge_load_timeout_ms;
	const paramsRef = React.useRef(params);
	paramsRef.current = params;
	const reportError = (err) => {
		const { onError } = paramsRef.current;
		if (onError) {
			onError(err);
			return;
		}
		try {
			handleError(err, [], card.setError);
		} catch {
			card.setError("Unable to complete action at this time. If the problem persists please contact support.");
		}
	};
	const containerRef = React.useRef(null);
	const isRunningRef = React.useRef(false);
	const runIdRef = React.useRef(0);
	const [isRunning, setIsRunning] = React.useState(false);
	const [isWidgetVisible, setIsWidgetVisibleState] = React.useState(false);
	const [retryNonce, setRetryNonce] = React.useState(0);
	const mountedRef = React.useRef(true);
	React.useEffect(() => {
		mountedRef.current = true;
		return () => {
			mountedRef.current = false;
		};
	}, []);
	const reloadCountRef = React.useRef(0);
	const token = params.getProtectCheck()?.token;
	React.useEffect(() => {
		const { getProtectCheck, onResolved } = paramsRef.current;
		if (!getProtectCheck() || isRunningRef.current) return;
		new AbortController();
		isRunningRef.current = false;
		setIsRunning(false);
		handleError(new ClerkRuntimeError("Protect verification is not supported in this environment", { code: ERROR_CODES.PROTECT_CHECK_UNSUPPORTED_ENVIRONMENT }), [], card.setError);
	}, [token, retryNonce]);
	const retry = React.useCallback(() => {
		card.setError("");
		isRunningRef.current = false;
		reloadCountRef.current = 0;
		const { getProtectCheck, getResource, onResolved } = paramsRef.current;
		if (!getProtectCheck()) {
			const runId = ++runIdRef.current;
			const ownsOutcome = () => mountedRef.current && runIdRef.current === runId;
			isRunningRef.current = true;
			setIsRunning(true);
			onResolved(getResource(), () => !mountedRef.current).catch((err) => {
				if (ownsOutcome()) reportError(err);
			}).finally(() => {
				if (ownsOutcome()) {
					isRunningRef.current = false;
					setIsRunning(false);
				}
			});
			return;
		}
		setRetryNonce((n) => n + 1);
	}, []);
	return {
		containerRef,
		isRunning,
		isWidgetVisible,
		error: card.error,
		retry
	};
}

//#endregion
export { useProtectCheckRunner };
//# sourceMappingURL=useProtectCheckRunner.js.map