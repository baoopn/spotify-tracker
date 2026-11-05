import SvgChevronLeft from "../../icons/chevron-left.js";
import { localizationKeys } from "../../localization/localizationKeys.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Icon, SimpleButton, Text } from "../../customizables/index.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/SecurityBackControl.tsx
const SecurityBackControl = ({ onClick }) => /* @__PURE__ */ jsxs(SimpleButton, {
	elementDescriptor: descriptors.configureSSOHeaderBackButton,
	variant: "unstyled",
	onClick,
	sx: (t) => ({
		gap: t.space.$1,
		padding: 0,
		color: t.colors.$colorMutedForeground,
		"&:hover": { color: t.colors.$colorForeground }
	}),
	children: [/* @__PURE__ */ jsx(Icon, { icon: SvgChevronLeft }), /* @__PURE__ */ jsx(Text, {
		as: "span",
		variant: "body",
		localizationKey: localizationKeys("organizationProfile.navbar.security")
	})]
});

//#endregion
export { SecurityBackControl };
//# sourceMappingURL=SecurityBackControl.js.map