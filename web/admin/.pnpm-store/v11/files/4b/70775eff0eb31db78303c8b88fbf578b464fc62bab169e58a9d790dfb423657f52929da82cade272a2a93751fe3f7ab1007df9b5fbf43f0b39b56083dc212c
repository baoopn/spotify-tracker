import { localizationKeys } from "../../../localization/localizationKeys.js";
import { isOidcProvider } from "./organizationEnterpriseConnection.js";

//#region src/components/ConfigureSSO/domain/providers.ts
const PROVIDER_GROUPS = [{
	id: "saml",
	label: localizationKeys("configureSSO.selectProviderStep.saml.groupLabel"),
	options: [
		{
			id: "saml_okta",
			label: localizationKeys("configureSSO.selectProviderStep.saml.okta"),
			iconId: "okta"
		},
		{
			id: "saml_microsoft",
			label: localizationKeys("configureSSO.selectProviderStep.saml.microsoft"),
			iconId: "microsoft"
		},
		{
			id: "saml_google",
			label: localizationKeys("configureSSO.selectProviderStep.saml.google"),
			iconId: "google"
		},
		{
			id: "saml_custom",
			label: localizationKeys("configureSSO.selectProviderStep.saml.customSaml"),
			iconId: "saml"
		}
	]
}, {
	id: "oidc",
	label: localizationKeys("configureSSO.selectProviderStep.oidc.groupLabel"),
	options: [{
		id: "oidc_custom",
		label: localizationKeys("configureSSO.selectProviderStep.oidc.oidcProvider"),
		iconId: "oidc"
	}]
}];
const providerLabel = (provider) => PROVIDER_GROUPS.flatMap((group) => group.options).find((option) => option.id === provider)?.label;
const providerIconId = (provider) => PROVIDER_GROUPS.flatMap((group) => group.options).find((option) => option.id === provider)?.iconId;
/** Every OIDC variant is presented as the single OIDC card. */
const toProviderCard = (provider) => isOidcProvider(provider) ? "oidc_custom" : provider;

//#endregion
export { PROVIDER_GROUPS, providerIconId, providerLabel, toProviderCard };
//# sourceMappingURL=providers.js.map