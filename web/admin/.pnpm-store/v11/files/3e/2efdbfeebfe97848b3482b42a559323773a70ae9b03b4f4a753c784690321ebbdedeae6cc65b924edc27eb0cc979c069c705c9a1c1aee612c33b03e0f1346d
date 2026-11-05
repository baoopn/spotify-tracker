import { localizationKeys } from "../../../localization/localizationKeys.js";
import SvgCheckmark from "../../../icons/checkmark.js";
import SvgClipboard from "../../../icons/clipboard.js";
import { useLocalizations } from "../../../localization/makeLocalizable.js";
import { Col, Text } from "../../../customizables/index.js";
import { ClipboardInput } from "../../../elements/ClipboardInput.js";
import { ProfileSection } from "../../../elements/Section.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/EnterpriseConnectionPage/ServiceProviderSection.tsx
const SamlServiceProviderSection = ({ connection }) => {
	const saml = connection.samlConnection;
	if (!saml) return null;
	return /* @__PURE__ */ jsxs(ServiceProviderSectionRoot, { children: [
		/* @__PURE__ */ jsx(CopyableValue, {
			label: localizationKeys("organizationProfile.securityPage.connectionPage.serviceProvider.acsUrl"),
			value: saml.acsUrl
		}),
		/* @__PURE__ */ jsx(CopyableValue, {
			label: localizationKeys("organizationProfile.securityPage.connectionPage.serviceProvider.entityId"),
			value: saml.spEntityId
		}),
		/* @__PURE__ */ jsx(CopyableValue, {
			label: localizationKeys("organizationProfile.securityPage.connectionPage.serviceProvider.metadataUrl"),
			value: saml.spMetadataUrl
		})
	] });
};
const OidcServiceProviderSection = ({ connection }) => {
	const redirectUri = connection.oauthConfig?.redirectUri;
	if (!redirectUri) return null;
	return /* @__PURE__ */ jsx(ServiceProviderSectionRoot, { children: /* @__PURE__ */ jsx(CopyableValue, {
		label: localizationKeys("organizationProfile.securityPage.connectionPage.serviceProvider.redirectUri"),
		value: redirectUri
	}) });
};
const ServiceProviderSectionRoot = ({ children }) => /* @__PURE__ */ jsx(ProfileSection.Root, {
	title: localizationKeys("organizationProfile.securityPage.connectionPage.serviceProvider.title"),
	id: "ssoConnectionServiceProvider",
	centered: false,
	children: /* @__PURE__ */ jsx(Col, {
		gap: 4,
		children
	})
});
const CopyableValue = ({ label, value }) => {
	const { t } = useLocalizations();
	return /* @__PURE__ */ jsxs(Col, {
		gap: 1,
		children: [/* @__PURE__ */ jsx(Text, {
			colorScheme: "secondary",
			variant: "caption",
			localizationKey: label
		}), /* @__PURE__ */ jsx(ClipboardInput, {
			value,
			readOnly: true,
			"aria-label": t(label),
			copyIcon: SvgClipboard,
			copiedIcon: SvgCheckmark
		})]
	});
};

//#endregion
export { OidcServiceProviderSection, SamlServiceProviderSection };
//# sourceMappingURL=ServiceProviderSection.js.map