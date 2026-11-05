import { localizationKeys } from "../../../localization/localizationKeys.js";
import { Text } from "../../../customizables/index.js";
import { ProfileSection } from "../../../elements/Section.js";
import { jsx } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/EnterpriseConnectionPage/DomainsSection.tsx
const DomainsSection = ({ connection }) => {
	if (connection.domains.length === 0) return null;
	return /* @__PURE__ */ jsx(ProfileSection.Root, {
		title: localizationKeys("organizationProfile.securityPage.connectionPage.domains.title"),
		id: "ssoConnectionDomains",
		centered: false,
		children: /* @__PURE__ */ jsx(ProfileSection.ItemList, {
			id: "ssoConnectionDomains",
			children: connection.domains.map((domain) => /* @__PURE__ */ jsx(ProfileSection.Item, {
				id: "ssoConnectionDomains",
				children: /* @__PURE__ */ jsx(Text, { children: domain })
			}, domain))
		})
	});
};

//#endregion
export { DomainsSection };
//# sourceMappingURL=DomainsSection.js.map