import { descriptors } from "../../../customizables/elementDescriptors.js";
import { Badge, Col, Flex, Text } from "../../../customizables/index.js";
import { Header } from "../../../elements/Header.js";
import { ProfileCard } from "../../../elements/ProfileCard/index.js";
import { isOidcProvider } from "../../ConfigureSSO/domain/organizationEnterpriseConnection.js";
import { providerLabel, toProviderCard } from "../../ConfigureSSO/domain/providers.js";
import { useOrganizationEnterpriseConnectionStatus } from "../../ConfigureSSO/hooks/useOrganizationEnterpriseConnectionStatus.js";
import { EnterpriseConnectionIcon } from "../EnterpriseConnectionIcon.js";
import { STATUS_BADGES } from "../enterpriseConnectionStatusBadges.js";
import { SecurityBackControl } from "../SecurityBackControl.js";
import { DomainsSection } from "./DomainsSection.js";
import { IdentityProviderSection } from "./IdentityProviderSection.js";
import { NameSection } from "./NameSection.js";
import { OidcServiceProviderSection, SamlServiceProviderSection } from "./ServiceProviderSection.js";
import { SettingsSection } from "./SettingsSection.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/EnterpriseConnectionPage/index.tsx
const EnterpriseConnectionPage = ({ connection, enterpriseConnectionMutations: { updateConnection }, onBack }) => {
	const isOidc = isOidcProvider(connection.provider);
	return /* @__PURE__ */ jsx(ProfileCard.Page, { children: /* @__PURE__ */ jsx(Col, {
		elementDescriptor: [descriptors.page, descriptors.organizationProfileSecuritySsoConnectionPage],
		sx: (t) => ({ gap: t.space.$8 }),
		children: /* @__PURE__ */ jsxs(Col, {
			elementDescriptor: descriptors.profilePage,
			elementId: descriptors.profilePage.setId("organizationSecurity"),
			children: [
				/* @__PURE__ */ jsx(ConnectionHeader, {
					connection,
					onBack
				}),
				/* @__PURE__ */ jsx(NameSection, {
					connection,
					updateConnection
				}),
				/* @__PURE__ */ jsx(DomainsSection, { connection }),
				isOidc ? /* @__PURE__ */ jsx(OidcServiceProviderSection, { connection }) : /* @__PURE__ */ jsx(SamlServiceProviderSection, { connection }),
				/* @__PURE__ */ jsx(IdentityProviderSection, {
					connection,
					updateConnection
				}),
				/* @__PURE__ */ jsx(SettingsSection, {
					connection,
					family: isOidc ? "oidc" : "saml",
					updateConnection
				})
			]
		})
	}) });
};
const ConnectionHeader = ({ connection, onBack }) => {
	const { status } = useOrganizationEnterpriseConnectionStatus(connection);
	const badge = STATUS_BADGES[status];
	const label = providerLabel(toProviderCard(connection.provider));
	return /* @__PURE__ */ jsxs(Col, {
		sx: (t) => ({
			gap: t.space.$4,
			marginBottom: t.space.$4
		}),
		children: [/* @__PURE__ */ jsx(Flex, { children: /* @__PURE__ */ jsx(SecurityBackControl, { onClick: onBack }) }), /* @__PURE__ */ jsxs(Flex, {
			align: "center",
			wrap: "wrap",
			sx: (t) => ({ gap: t.space.$2 }),
			children: [
				/* @__PURE__ */ jsx(EnterpriseConnectionIcon, { connection }),
				/* @__PURE__ */ jsxs(Col, {
					sx: { minWidth: 0 },
					children: [/* @__PURE__ */ jsx(Header.Title, {
						textVariant: "h2",
						children: connection.name
					}), label && /* @__PURE__ */ jsx(Text, {
						colorScheme: "secondary",
						variant: "caption",
						localizationKey: label
					})]
				}),
				/* @__PURE__ */ jsx(Badge, {
					elementDescriptor: descriptors.organizationProfileSecuritySsoBadge,
					elementId: descriptors.organizationProfileSecuritySsoBadge.setId(badge.id),
					colorScheme: badge.colorScheme,
					localizationKey: badge.label
				})
			]
		})]
	});
};

//#endregion
export { EnterpriseConnectionPage };
//# sourceMappingURL=index.js.map