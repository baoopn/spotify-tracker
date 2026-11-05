import { localizationKeys } from "../../localization/localizationKeys.js";
import { useLocalizations } from "../../localization/makeLocalizable.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Badge, Button, Col, Flex, Input, Text } from "../../customizables/index.js";
import { isEmail } from "../../utils/emailUtils.js";
import { useConfigureDirectorySync } from "./ConfigureDirectorySyncContext.js";
import { useCallback, useRef, useState } from "react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";
import { readJSONFile } from "@clerk/shared/file";

//#region src/components/ConfigureDirectorySync/GoogleCredentialsForm.tsx
/**
* Holds the credential a pull-based directory reads the identity provider with.
*
* State lives here rather than inside the form so the step footer can drive it:
* the wizard's Continue performs the submit, so the form has no button of its
* own and there is only one way forward.
*/
const useGoogleCredentialsState = () => {
	const { directory, setCredentials } = useConfigureDirectorySync();
	const { t } = useLocalizations();
	const [serviceAccountJson, setServiceAccountJson] = useState("");
	const [fileName, setFileName] = useState(null);
	const [subjectEmail, setSubjectEmail] = useState("");
	const [fileError, setFileError] = useState(null);
	const isConfigured = Boolean(directory?.credentialsConfigured);
	const trimmedSubjectEmail = subjectEmail.trim();
	const hasPendingCredential = Boolean(serviceAccountJson) && isEmail(trimmedSubjectEmail);
	const selectFile = useCallback(async (file) => {
		if (!file) return;
		setServiceAccountJson("");
		setFileName(null);
		setFileError(null);
		let key;
		try {
			key = await readJSONFile(file);
		} catch {
			setFileError(t(localizationKeys("configureDirectorySync.configureStep.error__invalidKeyFile")));
			return;
		}
		setServiceAccountJson(JSON.stringify(key));
		setFileName(file.name);
	}, [t]);
	const submit = useCallback(async () => {
		if (!hasPendingCredential) return;
		await setCredentials({
			serviceAccountJson,
			subjectEmail: trimmedSubjectEmail
		});
		setServiceAccountJson("");
		setFileName(null);
	}, [
		hasPendingCredential,
		setCredentials,
		serviceAccountJson,
		trimmedSubjectEmail
	]);
	return {
		fileName,
		subjectEmail,
		fileError,
		isConfigured,
		canContinue: isConfigured || hasPendingCredential,
		setSubjectEmail,
		selectFile,
		submit
	};
};
/**
* Collects the service account key and the administrator it impersonates. The
* key is never held in wizard state, which outlives the request.
*/
const GoogleCredentialsForm = ({ state }) => {
	const { t } = useLocalizations();
	const fileInputRef = useRef(null);
	const { fileName, subjectEmail, fileError, isConfigured, setSubjectEmail, selectFile } = state;
	return /* @__PURE__ */ jsxs(Col, {
		elementDescriptor: descriptors.configureDirectorySyncCredentialsForm,
		sx: (t) => ({ gap: t.space.$5 }),
		children: [
			/* @__PURE__ */ jsxs(Flex, {
				align: "center",
				sx: (t) => ({ gap: t.space.$2 }),
				children: [/* @__PURE__ */ jsx(Text, {
					as: "span",
					localizationKey: localizationKeys("configureDirectorySync.configureStep.formFieldLabel__serviceAccountKey"),
					sx: (t) => ({
						fontSize: t.fontSizes.$sm,
						fontWeight: t.fontWeights.$medium
					})
				}), /* @__PURE__ */ jsx(Badge, {
					elementDescriptor: descriptors.configureDirectorySyncCredentialsBadge,
					colorScheme: isConfigured ? "success" : "warning",
					localizationKey: localizationKeys(isConfigured ? "configureDirectorySync.configureStep.badge__credentialsConfigured" : "configureDirectorySync.configureStep.badge__credentialsMissing")
				})]
			}),
			/* @__PURE__ */ jsxs(Col, {
				sx: (t) => ({ gap: t.space.$1x5 }),
				children: [
					/* @__PURE__ */ jsx("input", {
						ref: fileInputRef,
						type: "file",
						accept: "application/json,.json",
						hidden: true,
						onChange: (event) => void selectFile(event.target.files?.[0])
					}),
					/* @__PURE__ */ jsxs(Flex, {
						align: "center",
						sx: (t) => ({ gap: t.space.$2 }),
						children: [/* @__PURE__ */ jsx(Button, {
							elementDescriptor: descriptors.configureDirectorySyncUploadKeyButton,
							variant: "outline",
							size: "sm",
							onClick: () => fileInputRef.current?.click(),
							localizationKey: localizationKeys(fileName ? "configureDirectorySync.configureStep.actionLabel__replaceKey" : "configureDirectorySync.configureStep.actionLabel__uploadKey")
						}), fileName && /* @__PURE__ */ jsx(Text, {
							elementDescriptor: descriptors.configureDirectorySyncUploadedFileName,
							as: "span",
							colorScheme: "secondary",
							sx: (t) => ({ fontSize: t.fontSizes.$sm }),
							children: fileName
						})]
					}),
					fileError && /* @__PURE__ */ jsx(Text, {
						as: "span",
						colorScheme: "danger",
						sx: (t) => ({ fontSize: t.fontSizes.$sm }),
						children: fileError
					})
				]
			}),
			/* @__PURE__ */ jsxs(Col, {
				sx: (t) => ({ gap: t.space.$1x5 }),
				children: [
					/* @__PURE__ */ jsx(Text, {
						as: "span",
						localizationKey: localizationKeys("configureDirectorySync.configureStep.formFieldLabel__subjectEmail"),
						sx: (t) => ({
							fontSize: t.fontSizes.$sm,
							fontWeight: t.fontWeights.$medium
						})
					}),
					/* @__PURE__ */ jsx(Input, {
						elementDescriptor: descriptors.configureDirectorySyncSubjectEmailInput,
						type: "email",
						value: subjectEmail,
						onChange: (event) => setSubjectEmail(event.target.value),
						placeholder: t(localizationKeys("configureDirectorySync.configureStep.formFieldInputPlaceholder__subjectEmail"))
					}),
					/* @__PURE__ */ jsx(Text, {
						as: "span",
						colorScheme: "secondary",
						localizationKey: localizationKeys("configureDirectorySync.configureStep.formFieldHint__subjectEmail"),
						sx: (t) => ({ fontSize: t.fontSizes.$sm })
					})
				]
			})
		]
	});
};

//#endregion
export { GoogleCredentialsForm, useGoogleCredentialsState };
//# sourceMappingURL=GoogleCredentialsForm.js.map