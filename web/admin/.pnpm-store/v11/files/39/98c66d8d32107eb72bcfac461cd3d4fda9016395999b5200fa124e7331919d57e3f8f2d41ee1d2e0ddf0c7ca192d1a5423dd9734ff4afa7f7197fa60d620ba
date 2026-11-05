import { descriptors } from "../../customizables/elementDescriptors.js";
import { ProviderIcon, getEnterpriseProviderIconId } from "../../common/ProviderIcon.js";
import { providerIconId, toProviderCard } from "../ConfigureSSO/domain/providers.js";
import { jsx } from "@emotion/react/jsx-runtime";
import { iconImageUrl } from "@clerk/shared/constants";

//#region src/components/OrganizationProfile/EnterpriseConnectionIcon.tsx
const EnterpriseConnectionIcon = ({ connection, size }) => {
	const iconId = providerIconId(toProviderCard(connection.provider));
	const iconUrl = iconId ? iconImageUrl(iconId) : connection.logoPublicUrl?.trim();
	return /* @__PURE__ */ jsx(ProviderIcon, {
		id: getEnterpriseProviderIconId(connection.provider),
		iconUrl,
		name: connection.name,
		size,
		elementDescriptor: descriptors.organizationProfileSecuritySsoProviderIcon
	});
};

//#endregion
export { EnterpriseConnectionIcon };
//# sourceMappingURL=EnterpriseConnectionIcon.js.map