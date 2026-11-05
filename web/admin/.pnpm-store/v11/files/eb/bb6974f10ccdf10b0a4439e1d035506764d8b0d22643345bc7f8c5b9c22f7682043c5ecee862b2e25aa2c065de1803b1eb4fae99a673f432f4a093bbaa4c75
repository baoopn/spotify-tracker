import { localizationKeys } from "../../localization/localizationKeys.js";
import { useSpinDelay } from "../../hooks/useSpinDelay.js";
import { useLocalizations } from "../../localization/makeLocalizable.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Flow } from "../../customizables/Flow.js";
import { Box, Button, Col, Flex, Spinner } from "../../customizables/index.js";
import { Card } from "../../elements/Card/index.js";
import { Header } from "../../elements/Header.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/ProtectCheck/ProtectCheckCard.tsx
const localizationKeysByFlow = {
	signIn: {
		title: localizationKeys("signIn.protectCheck.title"),
		subtitle: localizationKeys("signIn.protectCheck.subtitle"),
		loading: localizationKeys("signIn.protectCheck.loading"),
		retryButton: localizationKeys("signIn.protectCheck.retryButton")
	},
	signUp: {
		title: localizationKeys("signUp.protectCheck.title"),
		subtitle: localizationKeys("signUp.protectCheck.subtitle"),
		loading: localizationKeys("signUp.protectCheck.loading"),
		retryButton: localizationKeys("signUp.protectCheck.retryButton")
	}
};
const ProtectCheckCard = ({ flow, runner }) => {
	const { containerRef, isRunning, isWidgetVisible, error, retry } = runner;
	const { t } = useLocalizations();
	const keys = localizationKeysByFlow[flow];
	const showSpinner = useSpinDelay(isRunning, { delay: 300 });
	return /* @__PURE__ */ jsx(Flow.Part, {
		part: "protectCheck",
		children: /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsxs(Card.Content, { children: [
			/* @__PURE__ */ jsxs(Header.Root, {
				showLogo: true,
				children: [/* @__PURE__ */ jsx(Header.Title, { localizationKey: keys.title }), /* @__PURE__ */ jsx(Header.Subtitle, { localizationKey: keys.subtitle })]
			}),
			/* @__PURE__ */ jsx(Card.Alert, { children: error }),
			/* @__PURE__ */ jsxs(Col, {
				elementDescriptor: descriptors.main,
				gap: 6,
				children: [
					/* @__PURE__ */ jsx(Box, {
						ref: containerRef,
						id: "clerk-protect-check",
						"aria-busy": isRunning,
						style: {
							display: "block",
							alignSelf: "center",
							position: isWidgetVisible ? "static" : "absolute"
						}
					}),
					showSpinner && !error && !isWidgetVisible ? /* @__PURE__ */ jsx(Flex, {
						center: true,
						children: /* @__PURE__ */ jsx(Spinner, {
							size: "lg",
							colorScheme: "primary",
							elementDescriptor: descriptors.spinner,
							"aria-label": t(keys.loading)
						})
					}) : null,
					error ? /* @__PURE__ */ jsx(Button, {
						onClick: retry,
						localizationKey: keys.retryButton
					}) : null
				]
			})
		] }), /* @__PURE__ */ jsx(Card.Footer, {})] })
	});
};

//#endregion
export { ProtectCheckCard };
//# sourceMappingURL=ProtectCheckCard.js.map