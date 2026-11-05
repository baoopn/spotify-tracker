import { localizationKeys } from "../../localization/localizationKeys.js";

//#region src/components/ConfigureDirectorySync/providerMeta.ts
const instructionKeys = (provider) => [
	localizationKeys(`configureDirectorySync.configureStep.instructions.${provider}.step1`),
	localizationKeys(`configureDirectorySync.configureStep.instructions.${provider}.step2`),
	localizationKeys(`configureDirectorySync.configureStep.instructions.${provider}.step3`),
	localizationKeys(`configureDirectorySync.configureStep.instructions.${provider}.step4`)
];
const googleInstructionKeys = () => [
	localizationKeys("configureDirectorySync.configureStep.instructions.google.step1"),
	localizationKeys("configureDirectorySync.configureStep.instructions.google.step2"),
	localizationKeys("configureDirectorySync.configureStep.instructions.google.step3"),
	localizationKeys("configureDirectorySync.configureStep.instructions.google.step4"),
	localizationKeys("configureDirectorySync.configureStep.instructions.google.step5")
];
const DIRECTORY_SYNC_PROVIDERS = {
	okta: {
		name: localizationKeys("configureDirectorySync.providers.okta"),
		mode: "push",
		instructions: instructionKeys("okta")
	},
	entra: {
		name: localizationKeys("configureDirectorySync.providers.entra"),
		mode: "push",
		instructions: instructionKeys("entra")
	},
	google: {
		name: localizationKeys("configureDirectorySync.providers.google"),
		mode: "pull",
		instructions: googleInstructionKeys()
	},
	custom: {
		name: localizationKeys("configureDirectorySync.providers.custom"),
		mode: "push",
		instructions: instructionKeys("custom")
	}
};
/**
* Client-side mirror of the server's provider derivation: the directory's SCIM
* provider follows from the SSO connection's identity provider. Used for
* display before the directory exists; the server derives authoritatively on
* create.
*/
function directorySyncProviderForConnection(connectionProvider) {
	switch (connectionProvider) {
		case "saml_okta": return "okta";
		case "saml_microsoft": return "entra";
		case "saml_google": return "google";
		default: return "custom";
	}
}

//#endregion
export { DIRECTORY_SYNC_PROVIDERS, directorySyncProviderForConnection };
//# sourceMappingURL=providerMeta.js.map