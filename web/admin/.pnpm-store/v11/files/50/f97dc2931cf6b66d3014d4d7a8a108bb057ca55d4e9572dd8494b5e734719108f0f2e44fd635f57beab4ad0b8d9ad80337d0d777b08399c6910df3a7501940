import { localizationKeys } from "../../../localization/localizationKeys.js";
import { useFormControl } from "../../../utils/useFormControl.js";
import { useCardState, withCardStateProvider } from "../../../elements/contexts/index.js";
import { Col, Text } from "../../../customizables/index.js";
import { handleError } from "../../../utils/errorHandler.js";
import { Form } from "../../../elements/Form.js";
import { FormButtons } from "../../../elements/FormButtons.js";
import { FormContainer } from "../../../elements/FormContainer.js";
import { useActionContext } from "../../../elements/Action/ActionRoot.js";
import { Action } from "../../../elements/Action/index.js";
import { ProfileSection } from "../../../elements/Section.js";
import { formatDate } from "../../../utils/formatDate.js";
import { isOidcProvider } from "../../ConfigureSSO/domain/organizationEnterpriseConnection.js";
import { IdentityProviderConfigurationModes } from "../../ConfigureSSO/steps/ConfigureStep/shared/IdentityProviderConfigurationModes.js";
import { OidcEndpointsConfigurationForm } from "../../ConfigureSSO/steps/ConfigureStep/oidc/shared/OidcEndpointsConfigurationForm.js";
import { getIdpCertificateStatus, toIdpCertificateEntries } from "../../ConfigureSSO/domain/idpCertificates.js";
import { IdentityProviderConfigurationForm, applySamlSubmitError, buildSamlConfigurationPayload } from "../../ConfigureSSO/steps/ConfigureStep/saml/shared/IdentityProviderConfigurationForm.js";
import { useState } from "react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/EnterpriseConnectionPage/IdentityProviderSection.tsx
const SAML_MODES = ["metadataUrl", "manual"];
const OIDC_MODES = ["discoveryUrl", "manual"];
const samlDetails = (connection) => {
	const saml = connection.samlConnection;
	const details = saml?.idpMetadataUrl ? [{
		id: "idpMetadataUrl",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.metadataUrl.label"),
		value: saml.idpMetadataUrl
	}] : [{
		id: "idpSsoUrl",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signOnUrl.label"),
		value: saml?.idpSsoUrl ?? ""
	}, {
		id: "idpEntityId",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.issuer.label"),
		value: saml?.idpEntityId ?? ""
	}];
	const certificates = toIdpCertificateEntries(saml);
	if (certificates.length > 0) details.push(certificatesDetail(certificates));
	return details;
};
const toneFor = (entry) => {
	const status = getIdpCertificateStatus(entry);
	return status === "expired" ? "danger" : status === "expiring" ? "warning" : void 0;
};
const certificatesDetail = (certificates) => {
	const dated = certificates.filter((entry) => entry.expiresAt !== null);
	const earliest = dated.length > 0 ? dated.reduce((a, b) => (b.expiresAt ?? 0) < (a.expiresAt ?? 0) ? b : a) : null;
	if (certificates.length === 1) {
		const [only] = certificates;
		return {
			id: "idpCertificateExpiresAt",
			label: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.certificateExpires"),
			value: only.expiresAt === null ? void 0 : formatDate(new Date(only.expiresAt)),
			tone: toneFor(only)
		};
	}
	const count = certificates.length;
	if (!earliest || earliest.expiresAt === null) return {
		id: "idpCertificates",
		label: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.certificates"),
		valueKey: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.certificatesCount", { count })
	};
	const expired = getIdpCertificateStatus(earliest) === "expired";
	return {
		id: "idpCertificates",
		label: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.certificates"),
		valueKey: localizationKeys(expired ? "organizationProfile.securityPage.connectionPage.identityProvider.certificatesSummaryExpired" : "organizationProfile.securityPage.connectionPage.identityProvider.certificatesSummary", {
			count,
			date: formatDate(new Date(earliest.expiresAt))
		}),
		tone: toneFor(earliest)
	};
};
const oidcDetails = (connection) => {
	const oauthConfig = connection.oauthConfig;
	return [{
		id: "clientId",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.credentialsStep.clientId.label"),
		value: oauthConfig?.clientId ?? ""
	}, ...oauthConfig?.discoveryUrl ? [{
		id: "discoveryUrl",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.discoveryUrl.label"),
		value: oauthConfig.discoveryUrl
	}] : [
		{
			id: "authUrl",
			label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.authUrl.label"),
			value: oauthConfig?.authUrl ?? ""
		},
		{
			id: "tokenUrl",
			label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.tokenUrl.label"),
			value: oauthConfig?.tokenUrl ?? ""
		},
		{
			id: "userInfoUrl",
			label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.userInfoUrl.label"),
			value: oauthConfig?.userInfoUrl ?? ""
		}
	]];
};
const IdentityProviderSection = (props) => {
	const isOidc = isOidcProvider(props.connection.provider);
	const details = isOidc ? oidcDetails(props.connection) : samlDetails(props.connection);
	return /* @__PURE__ */ jsx(ProfileSection.Root, {
		title: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.title"),
		id: "ssoConnectionIdentityProvider",
		centered: false,
		children: /* @__PURE__ */ jsxs(Action.Root, { children: [/* @__PURE__ */ jsx(Action.Closed, {
			value: "edit",
			children: /* @__PURE__ */ jsxs(ProfileSection.Item, {
				id: "ssoConnectionIdentityProvider",
				children: [/* @__PURE__ */ jsx(Col, {
					sx: (t) => ({
						gap: t.space.$3,
						minWidth: 0
					}),
					children: details.filter((detail) => detail.value || detail.valueKey).map((detail) => /* @__PURE__ */ jsxs(Col, {
						sx: (t) => ({ gap: t.space.$0x5 }),
						children: [/* @__PURE__ */ jsx(Text, {
							colorScheme: "secondary",
							variant: "caption",
							localizationKey: detail.label
						}), /* @__PURE__ */ jsx(Text, {
							sx: { overflowWrap: "anywhere" },
							colorScheme: detail.tone,
							localizationKey: detail.valueKey,
							children: detail.value
						})]
					}, detail.id))
				}), /* @__PURE__ */ jsx(Action.Trigger, {
					value: "edit",
					children: /* @__PURE__ */ jsx(ProfileSection.Button, {
						id: "ssoConnectionIdentityProvider",
						localizationKey: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.editButton")
					})
				})]
			})
		}), /* @__PURE__ */ jsx(Action.Open, {
			value: "edit",
			children: /* @__PURE__ */ jsx(Action.Card, { children: /* @__PURE__ */ jsx(IdentityProviderScreen, {
				...props,
				isOidc
			}) })
		})] })
	});
};
const IdentityProviderScreen = ({ isOidc, ...props }) => {
	const { close } = useActionContext();
	return isOidc ? /* @__PURE__ */ jsx(OidcForm, {
		...props,
		onSuccess: close,
		onReset: close
	}) : /* @__PURE__ */ jsx(SamlForm, {
		...props,
		onSuccess: close,
		onReset: close
	});
};
const SamlForm = withCardStateProvider(({ connection, updateConnection, onSuccess, onReset }) => {
	const card = useCardState();
	const saml = connection.samlConnection;
	const initialCertificates = toIdpCertificateEntries(saml);
	const [mode, setMode] = useState(saml?.idpMetadataUrl ? "metadataUrl" : "manual");
	const [certificates, setCertificates] = useState(initialCertificates);
	const metadataUrlField = useFormControl("idpMetadataUrl", saml?.idpMetadataUrl ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.metadataUrl.label"),
		placeholder: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.metadataUrl.placeholder"),
		isRequired: true
	});
	const signOnUrlField = useFormControl("idpSsoUrl", saml?.idpSsoUrl ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signOnUrl.label"),
		placeholder: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signOnUrl.placeholder"),
		isRequired: true
	});
	const issuerField = useFormControl("idpEntityId", saml?.idpEntityId ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.issuer.label"),
		placeholder: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.issuer.placeholder"),
		isRequired: true
	});
	const certificateField = useFormControl("idpCertificate", "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signingCertificate.label"),
		isRequired: true
	});
	const isValid = mode === "metadataUrl" ? metadataUrlField.value.trim().length > 0 : signOnUrlField.value.trim().length > 0 && issuerField.value.trim().length > 0 && certificates.length > 0;
	const formProps = mode === "metadataUrl" ? {
		mode: "metadataUrl",
		form: { field: metadataUrlField },
		labels: { description: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.metadataUrl.description") }
	} : {
		mode: "manual",
		form: {
			signOnUrlField,
			issuerField,
			certificateField,
			certificates,
			onCertificatesChange: setCertificates,
			initialCertificates
		},
		labels: {
			description: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.description"),
			uploadFile: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signingCertificate.uploadFile"),
			replaceFile: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signingCertificate.replaceFile"),
			removeFile: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signingCertificate.removeFile"),
			fileUploaded: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.manual.signingCertificate.fileUploaded")
		}
	};
	const onSubmit = async (e) => {
		e.preventDefault();
		if (!isValid || card.isLoading) return;
		card.setError(void 0);
		try {
			const payload = await buildSamlConfigurationPayload({
				mode,
				metadataUrl: { value: metadataUrlField.value },
				manual: {
					signOnUrl: signOnUrlField.value,
					issuer: issuerField.value,
					certificates,
					initialCertificates
				}
			});
			await updateConnection(connection.id, { saml: payload });
			onSuccess();
		} catch (err) {
			if (mode === "metadataUrl") applySamlSubmitError(err, card, metadataUrlField);
			else applySamlSubmitError(err, card, signOnUrlField, [issuerField, certificateField]);
		}
	};
	return /* @__PURE__ */ jsx(FormContainer, {
		headerTitle: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.form.title"),
		children: /* @__PURE__ */ jsxs(Form.Root, {
			onSubmit,
			children: [
				/* @__PURE__ */ jsx(IdentityProviderConfigurationModes, {
					modes: SAML_MODES,
					value: mode,
					onChange: (next) => {
						card.setError(void 0);
						setMode(next);
					},
					labels: {
						ariaLabel: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.modes.ariaLabel"),
						metadataUrl: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.modes.metadataUrl"),
						manual: localizationKeys("configureSSO.configureStep.samlCustom.identityProviderMetadataStep.modes.manual")
					}
				}),
				/* @__PURE__ */ jsx(IdentityProviderConfigurationForm, { ...formProps }),
				/* @__PURE__ */ jsx(FormButtons, {
					isDisabled: !isValid || card.isLoading,
					onReset
				})
			]
		})
	});
});
const OidcForm = withCardStateProvider(({ connection, updateConnection, onSuccess, onReset }) => {
	const card = useCardState();
	const oauthConfig = connection.oauthConfig;
	const [mode, setMode] = useState(oauthConfig?.discoveryUrl ? "discoveryUrl" : "manual");
	const clientIdField = useFormControl("clientId", oauthConfig?.clientId ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.credentialsStep.clientId.label"),
		placeholder: localizationKeys("configureSSO.configureStep.oidcCustom.credentialsStep.clientId.placeholder"),
		isRequired: true
	});
	const clientSecretField = useFormControl("clientSecret", "", {
		type: "password",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.credentialsStep.clientSecret.label"),
		placeholder: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.clientSecret.placeholder")
	});
	const discoveryUrlField = useFormControl("discoveryUrl", oauthConfig?.discoveryUrl ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.discoveryUrl.label"),
		placeholder: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.discoveryUrl.placeholder"),
		isRequired: true
	});
	const authUrlField = useFormControl("authUrl", oauthConfig?.authUrl ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.authUrl.label"),
		placeholder: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.authUrl.placeholder"),
		isRequired: true
	});
	const tokenUrlField = useFormControl("tokenUrl", oauthConfig?.tokenUrl ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.tokenUrl.label"),
		placeholder: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.tokenUrl.placeholder"),
		isRequired: true
	});
	const userInfoUrlField = useFormControl("userInfoUrl", oauthConfig?.userInfoUrl ?? "", {
		type: "text",
		label: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.userInfoUrl.label"),
		placeholder: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.userInfoUrl.placeholder")
	});
	const isValid = clientIdField.value.trim().length > 0 && (mode === "discoveryUrl" ? discoveryUrlField.value.trim().length > 0 : authUrlField.value.trim().length > 0 && tokenUrlField.value.trim().length > 0);
	const endpointsProps = mode === "discoveryUrl" ? {
		mode: "discoveryUrl",
		form: { discoveryUrlField },
		labels: { description: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.discoveryUrl.description") }
	} : {
		mode: "manual",
		form: {
			authUrlField,
			tokenUrlField,
			userInfoUrlField
		},
		labels: { description: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.manual.description") }
	};
	const onSubmit = async (e) => {
		e.preventDefault();
		if (!isValid || card.isLoading) return;
		card.setError(void 0);
		try {
			await updateConnection(connection.id, { oidc: {
				clientId: clientIdField.value.trim(),
				clientSecret: clientSecretField.value.trim() || void 0,
				...mode === "discoveryUrl" ? { discoveryUrl: discoveryUrlField.value.trim() } : {
					authUrl: authUrlField.value.trim(),
					tokenUrl: tokenUrlField.value.trim(),
					userInfoUrl: userInfoUrlField.value.trim()
				}
			} });
			onSuccess();
		} catch (err) {
			handleError(err, [clientIdField, clientSecretField], card.setError);
		}
	};
	return /* @__PURE__ */ jsx(FormContainer, {
		headerTitle: localizationKeys("organizationProfile.securityPage.connectionPage.identityProvider.form.title"),
		children: /* @__PURE__ */ jsxs(Form.Root, {
			onSubmit,
			children: [
				/* @__PURE__ */ jsx(Form.ControlRow, {
					elementId: clientIdField.id,
					children: /* @__PURE__ */ jsx(Form.PlainInput, { ...clientIdField.props })
				}),
				/* @__PURE__ */ jsx(Form.ControlRow, {
					elementId: clientSecretField.id,
					children: /* @__PURE__ */ jsx(Form.PasswordInput, { ...clientSecretField.props })
				}),
				/* @__PURE__ */ jsx(IdentityProviderConfigurationModes, {
					modes: OIDC_MODES,
					value: mode,
					onChange: (next) => {
						card.setError(void 0);
						setMode(next);
					},
					labels: {
						ariaLabel: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.modes.ariaLabel"),
						discoveryUrl: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.modes.discoveryUrl"),
						manual: localizationKeys("configureSSO.configureStep.oidcCustom.endpointsStep.modes.manual")
					}
				}),
				/* @__PURE__ */ jsx(OidcEndpointsConfigurationForm, { ...endpointsProps }),
				/* @__PURE__ */ jsx(FormButtons, {
					isDisabled: !isValid || card.isLoading,
					onReset
				})
			]
		})
	});
});

//#endregion
export { IdentityProviderSection };
//# sourceMappingURL=IdentityProviderSection.js.map