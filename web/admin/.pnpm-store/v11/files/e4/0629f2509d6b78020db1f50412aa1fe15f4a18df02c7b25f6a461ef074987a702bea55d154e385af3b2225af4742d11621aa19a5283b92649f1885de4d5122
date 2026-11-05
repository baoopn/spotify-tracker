import { localizationKeys } from "../../localization/localizationKeys.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Badge, Col, Flex, Spinner, Text } from "../../customizables/index.js";
import { Alert } from "../../elements/Alert.js";
import { ThreeDotsMenu } from "../../elements/ThreeDotsMenu.js";
import { ProfileSection } from "../../elements/Section.js";
import { isClerkAPIResponseError } from "@clerk/shared/error";
import { __internal_useOrganizationSSOBypassAllowlist } from "@clerk/shared/react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/SecuritySSOBypassSection.tsx
const isFeatureNotEnabledError = (error) => error !== null && isClerkAPIResponseError(error) && error.errors.some((e) => e.code === "feature_not_enabled");
const SecuritySSOBypassSection = ({ onManage }) => {
	const { data, isLoading, error } = __internal_useOrganizationSSOBypassAllowlist();
	if (isFeatureNotEnabledError(error)) return null;
	const count = data?.length ?? 0;
	return /* @__PURE__ */ jsx(ProfileSection.Root, {
		title: localizationKeys("organizationProfile.securityPage.ssoBypassSection.title"),
		id: "ssoBypass",
		centered: false,
		children: /* @__PURE__ */ jsxs(Col, {
			gap: 4,
			children: [/* @__PURE__ */ jsxs(Flex, {
				align: "center",
				justify: "between",
				gap: 3,
				children: [isLoading ? /* @__PURE__ */ jsx(Spinner, {
					size: "xs",
					colorScheme: "neutral",
					elementDescriptor: descriptors.spinner
				}) : error ? /* @__PURE__ */ jsx(Alert, {
					variant: "danger",
					title: localizationKeys("organizationProfile.securityPage.ssoBypassSection.error__load"),
					subtitle: error.message
				}) : /* @__PURE__ */ jsxs(Flex, {
					align: "center",
					gap: 2,
					children: [/* @__PURE__ */ jsx(Text, {
						as: "span",
						elementDescriptor: descriptors.organizationProfileSecuritySsoBypassCountLabel,
						localizationKey: localizationKeys("organizationProfile.securityPage.ssoBypassSection.allowlistLabel")
					}), /* @__PURE__ */ jsx(Badge, {
						elementDescriptor: descriptors.organizationProfileSecuritySsoBypassCountBadge,
						localizationKey: count === 1 ? localizationKeys("organizationProfile.securityPage.ssoBypassSection.allowlistCount__one") : localizationKeys("organizationProfile.securityPage.ssoBypassSection.allowlistCount", { count: String(count) })
					})]
				}), /* @__PURE__ */ jsx(ThreeDotsMenu, {
					elementId: "ssoBypass",
					actions: [{
						label: localizationKeys("organizationProfile.securityPage.ssoBypassSection.menuAction__manage"),
						onClick: onManage
					}]
				})]
			}), /* @__PURE__ */ jsx(Text, {
				as: "p",
				elementDescriptor: descriptors.organizationProfileSecuritySsoBypassDescription,
				colorScheme: "secondary",
				localizationKey: localizationKeys("organizationProfile.securityPage.ssoBypassSection.description")
			})]
		})
	});
};

//#endregion
export { SecuritySSOBypassSection };
//# sourceMappingURL=SecuritySSOBypassSection.js.map