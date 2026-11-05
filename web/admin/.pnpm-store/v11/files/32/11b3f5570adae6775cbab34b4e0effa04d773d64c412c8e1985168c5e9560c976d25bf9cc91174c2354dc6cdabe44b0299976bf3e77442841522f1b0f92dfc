import { localizationKeys } from "../../../localization/localizationKeys.js";
import { useFormControl } from "../../../utils/useFormControl.js";
import { useCardState, withCardStateProvider } from "../../../elements/contexts/index.js";
import { Text } from "../../../customizables/index.js";
import { handleError } from "../../../utils/errorHandler.js";
import { Form } from "../../../elements/Form.js";
import { FormButtons } from "../../../elements/FormButtons.js";
import { FormContainer } from "../../../elements/FormContainer.js";
import { useActionContext } from "../../../elements/Action/ActionRoot.js";
import { Action } from "../../../elements/Action/index.js";
import { ProfileSection } from "../../../elements/Section.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/EnterpriseConnectionPage/NameSection.tsx
const NameSection = ({ connection, updateConnection }) => /* @__PURE__ */ jsx(ProfileSection.Root, {
	title: localizationKeys("organizationProfile.securityPage.connectionPage.name.title"),
	id: "ssoConnectionName",
	centered: false,
	children: /* @__PURE__ */ jsxs(Action.Root, { children: [/* @__PURE__ */ jsx(Action.Closed, {
		value: "name",
		children: /* @__PURE__ */ jsxs(ProfileSection.Item, {
			id: "ssoConnectionName",
			children: [/* @__PURE__ */ jsx(Text, { children: connection.name }), /* @__PURE__ */ jsx(Action.Trigger, {
				value: "name",
				children: /* @__PURE__ */ jsx(ProfileSection.Button, {
					id: "ssoConnectionName",
					localizationKey: localizationKeys("organizationProfile.securityPage.connectionPage.name.editButton")
				})
			})]
		})
	}), /* @__PURE__ */ jsx(Action.Open, {
		value: "name",
		children: /* @__PURE__ */ jsx(Action.Card, { children: /* @__PURE__ */ jsx(NameScreen, {
			connection,
			updateConnection
		}) })
	})] })
});
const NameScreen = (props) => {
	const { close } = useActionContext();
	return /* @__PURE__ */ jsx(NameForm, {
		...props,
		onSuccess: close,
		onReset: close
	});
};
const NameForm = withCardStateProvider(({ connection, updateConnection, onSuccess, onReset }) => {
	const card = useCardState();
	const nameField = useFormControl("name", connection.name, {
		type: "text",
		label: localizationKeys("organizationProfile.securityPage.connectionPage.name.title"),
		isRequired: true
	});
	const name = nameField.value.trim();
	const canSubmit = name.length > 0 && name !== connection.name;
	const onSubmit = async (e) => {
		e.preventDefault();
		if (!canSubmit || card.isLoading) return;
		try {
			await updateConnection(connection.id, { name });
			onSuccess();
		} catch (err) {
			handleError(err, [nameField], card.setError);
		}
	};
	return /* @__PURE__ */ jsx(FormContainer, {
		headerTitle: localizationKeys("organizationProfile.securityPage.connectionPage.name.form.title"),
		children: /* @__PURE__ */ jsxs(Form.Root, {
			onSubmit,
			children: [/* @__PURE__ */ jsx(Form.ControlRow, {
				elementId: nameField.id,
				children: /* @__PURE__ */ jsx(Form.PlainInput, {
					...nameField.props,
					autoFocus: true,
					ignorePasswordManager: true
				})
			}), /* @__PURE__ */ jsx(FormButtons, {
				isDisabled: !canSubmit || card.isLoading,
				onReset
			})]
		})
	});
});

//#endregion
export { NameSection };
//# sourceMappingURL=NameSection.js.map