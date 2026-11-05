import { Route } from "../../router/Route.js";
import { Switch } from "../../router/Switch.js";
import { Flow } from "../../customizables/Flow.js";
import { withCardStateProvider } from "../../elements/contexts/index.js";
import { useProtectCheckRunner } from "../../hooks/useProtectCheckRunner.js";
import { ProtectCheckCard } from "../ProtectCheck/ProtectCheckCard.js";
import { useEffect, useRef } from "react";
import { jsx } from "@emotion/react/jsx-runtime";

//#region src/components/ProtectCheckModal/index.tsx
const flowOf = (resource) => resource.pathRoot.endsWith("sign_ups") ? "signUp" : "signIn";
const ProtectCheckModalCard = withCardStateProvider(({ resource, onResolved, onFailed }) => {
	const runner = useProtectCheckRunner({
		getProtectCheck: () => resource.protectCheck,
		getResource: () => resource,
		reload: () => resource.reload(),
		submitProtectCheck: (params) => resource.submitProtectCheck(params),
		onResolved: (updated, isCancelled) => {
			if (!isCancelled() && !updated.protectCheck) onResolved();
			return Promise.resolve();
		},
		onError: onFailed
	});
	return /* @__PURE__ */ jsx(ProtectCheckCard, {
		flow: flowOf(resource),
		runner
	});
});
function ProtectCheckModal(props) {
	const { resource, onResolved } = props;
	const isClearOnMount = useRef(!resource.protectCheck).current;
	const didReportClearRef = useRef(false);
	useEffect(() => {
		if (isClearOnMount && !didReportClearRef.current) {
			didReportClearRef.current = true;
			onResolved();
		}
	}, [isClearOnMount, onResolved]);
	if (isClearOnMount) return null;
	return /* @__PURE__ */ jsx(Route, {
		path: "protect-check",
		children: /* @__PURE__ */ jsx("div", { children: /* @__PURE__ */ jsx(Flow.Root, {
			flow: "protectCheck",
			children: /* @__PURE__ */ jsx(Switch, { children: /* @__PURE__ */ jsx(Route, {
				index: true,
				children: /* @__PURE__ */ jsx(ProtectCheckModalCard, { ...props })
			}) })
		}) })
	});
}
ProtectCheckModal.displayName = "ProtectCheckModal";

//#endregion
export { ProtectCheckModal };
//# sourceMappingURL=index.js.map