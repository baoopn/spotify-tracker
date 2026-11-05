import { localizationKeys } from "../../../localization/localizationKeys.js";
import { useFormControl } from "../../../utils/useFormControl.js";
import { CardStateProvider, useCardState } from "../../../elements/contexts/index.js";
import { Card } from "../../../elements/Card/index.js";
import { handleError } from "../../../utils/errorHandler.js";
import { Form } from "../../../elements/Form.js";
import { FormButtons } from "../../../elements/FormButtons.js";
import { ProfileSection } from "../../../elements/Section.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/EnterpriseConnectionPage/SettingsSection.tsx
const SETTINGS = [
	{
		id: "syncUserAttributes",
		appliesTo: "all",
		isChecked: (connection) => Boolean(connection.syncUserAttributes),
		toParams: (syncUserAttributes) => ({ syncUserAttributes })
	},
	{
		id: "allowAdditionalIdentifiers",
		appliesTo: "all",
		isChecked: (connection) => !connection.disableAdditionalIdentifications,
		toParams: (checked) => ({ disableAdditionalIdentifications: !checked })
	},
	{
		id: "allowSubdomains",
		appliesTo: "saml",
		isChecked: (connection) => Boolean(connection.samlConnection?.allowSubdomains),
		toParams: (allowSubdomains) => ({ saml: { allowSubdomains } })
	},
	{
		id: "allowIdpInitiated",
		appliesTo: "saml",
		isChecked: (connection) => Boolean(connection.samlConnection?.allowIdpInitiated),
		toParams: (allowIdpInitiated) => ({ saml: { allowIdpInitiated } })
	},
	{
		id: "forceAuthn",
		appliesTo: "saml",
		isChecked: (connection) => Boolean(connection.samlConnection?.forceAuthn),
		toParams: (forceAuthn) => ({ saml: { forceAuthn } })
	}
];
const mergeParams = (params) => params.reduce((merged, next) => ({
	...merged,
	...next,
	...merged.saml || next.saml ? { saml: {
		...merged.saml,
		...next.saml
	} } : {}
}), {});
const settingById = (id) => SETTINGS.find((setting) => setting.id === id);
const useSettingField = (id, connection) => useFormControl(id, "", {
	type: "checkbox",
	label: localizationKeys(`organizationProfile.securityPage.connectionPage.settings.${id}.label`),
	defaultChecked: settingById(id).isChecked(connection)
});
const settingsSignature = (connection) => SETTINGS.map((setting) => setting.isChecked(connection) ? "1" : "0").join("");
const settingDescription = (id) => localizationKeys(`organizationProfile.securityPage.connectionPage.settings.${id}.description`);
const SettingsSection = ({ connection, family, updateConnection }) => /* @__PURE__ */ jsx(ProfileSection.Root, {
	title: localizationKeys("organizationProfile.securityPage.connectionPage.settings.title"),
	id: "ssoConnectionSettings",
	centered: false,
	children: /* @__PURE__ */ jsx(CardStateProvider, { children: /* @__PURE__ */ jsx(SettingsForm, {
		connection,
		family,
		updateConnection
	}, settingsSignature(connection)) })
});
const SettingsForm = ({ connection, family, updateConnection }) => {
	const card = useCardState();
	const fields = {
		syncUserAttributes: useSettingField("syncUserAttributes", connection),
		allowAdditionalIdentifiers: useSettingField("allowAdditionalIdentifiers", connection),
		allowSubdomains: useSettingField("allowSubdomains", connection),
		allowIdpInitiated: useSettingField("allowIdpInitiated", connection),
		forceAuthn: useSettingField("forceAuthn", connection)
	};
	const applicable = SETTINGS.filter((setting) => setting.appliesTo === "all" || setting.appliesTo === family);
	const changed = applicable.filter((setting) => Boolean(fields[setting.id].checked) !== setting.isChecked(connection));
	const onReset = () => {
		card.setError(void 0);
		applicable.forEach((setting) => fields[setting.id].setChecked(setting.isChecked(connection)));
	};
	const onSubmit = async (e) => {
		e.preventDefault();
		if (changed.length === 0 || card.isLoading) return;
		card.setError(void 0);
		card.setLoading();
		try {
			await updateConnection(connection.id, mergeParams(changed.map((setting) => setting.toParams(Boolean(fields[setting.id].checked)))));
		} catch (err) {
			handleError(err, [], card.setError);
		} finally {
			card.setIdle();
		}
	};
	return /* @__PURE__ */ jsxs(Form.Root, {
		onSubmit,
		children: [
			applicable.map((setting) => /* @__PURE__ */ jsx(Form.ControlRow, {
				elementId: setting.id,
				children: /* @__PURE__ */ jsx(Form.Checkbox, {
					...fields[setting.id].props,
					description: settingDescription(setting.id)
				})
			}, setting.id)),
			/* @__PURE__ */ jsx(Card.Alert, { children: card.error }),
			/* @__PURE__ */ jsx(FormButtons, {
				isDisabled: changed.length === 0 || card.isLoading,
				onReset
			})
		]
	});
};

//#endregion
export { SettingsSection };
//# sourceMappingURL=SettingsSection.js.map