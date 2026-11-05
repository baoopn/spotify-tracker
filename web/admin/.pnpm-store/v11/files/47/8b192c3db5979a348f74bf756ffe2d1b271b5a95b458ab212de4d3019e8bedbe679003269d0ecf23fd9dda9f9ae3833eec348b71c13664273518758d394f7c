import { localizationKeys } from "../../../../../../localization/localizationKeys.js";
import SvgArrowUpTray from "../../../../../../icons/arrow-up-tray.js";
import SvgClose from "../../../../../../icons/close.js";
import SvgExclamationTriangle from "../../../../../../icons/exclamation-triangle.js";
import { useLocalizations } from "../../../../../../localization/makeLocalizable.js";
import { descriptors } from "../../../../../../customizables/elementDescriptors.js";
import { Badge, Box, Button, Col, Flex, Icon, Span, Text } from "../../../../../../customizables/index.js";
import { handleError } from "../../../../../../utils/errorHandler.js";
import { Field } from "../../../../../../elements/FieldControl.js";
import { Form } from "../../../../../../elements/Form.js";
import { formatDate } from "../../../../../../utils/formatDate.js";
import { Tooltip } from "../../../../../../elements/Tooltip.js";
import { addCertificates, areCertificateBodies, getIdpCertificateStatus, haveCertificatesChanged, parseCertificateFile, removeCertificate, toIdpCertificatesParam } from "../../../../domain/idpCertificates.js";
import { isClerkAPIResponseError } from "@clerk/shared/error";
import React from "react";
import { Fragment as Fragment$1, jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/ConfigureSSO/steps/ConfigureStep/saml/shared/IdentityProviderConfigurationForm.tsx
const IdentityProviderConfigurationForm = (config) => {
	switch (config.mode) {
		case "metadataUrl": return /* @__PURE__ */ jsx(MetadataUrlPanel, {
			form: config.form,
			labels: config.labels
		});
		case "metadataFile": return /* @__PURE__ */ jsx(MetadataFilePanel, {
			form: config.form,
			labels: config.labels
		});
		case "manual": return /* @__PURE__ */ jsx(ManualPanel, {
			form: config.form,
			labels: config.labels
		});
	}
};
const MetadataUrlPanel = ({ form, labels }) => /* @__PURE__ */ jsxs(Fragment$1, { children: [/* @__PURE__ */ jsx(Text, {
	as: "p",
	colorScheme: "secondary",
	localizationKey: labels.description
}), /* @__PURE__ */ jsx(Form.ControlRow, {
	elementId: form.field.id,
	children: /* @__PURE__ */ jsx(Form.PlainInput, { ...form.field.props })
})] });
const MetadataFilePanel = ({ form, labels }) => /* @__PURE__ */ jsxs(Fragment$1, { children: [/* @__PURE__ */ jsx(Text, {
	as: "p",
	colorScheme: "secondary",
	localizationKey: labels.description
}), /* @__PURE__ */ jsx(FileUploadField, {
	field: form.field,
	file: form.file,
	onFileChange: form.onFileChange,
	existingFilePresent: Boolean(form.existingFilePresent),
	labels,
	accept: ".xml"
})] });
const ManualPanel = ({ form, labels }) => /* @__PURE__ */ jsxs(Fragment$1, { children: [
	/* @__PURE__ */ jsx(Text, {
		as: "p",
		colorScheme: "secondary",
		localizationKey: labels.description
	}),
	/* @__PURE__ */ jsx(Form.ControlRow, {
		elementId: form.signOnUrlField.id,
		children: /* @__PURE__ */ jsx(Form.PlainInput, { ...form.signOnUrlField.props })
	}),
	/* @__PURE__ */ jsx(Form.ControlRow, {
		elementId: form.issuerField.id,
		children: /* @__PURE__ */ jsx(Form.PlainInput, { ...form.issuerField.props })
	}),
	/* @__PURE__ */ jsx(CertificateListField, {
		field: form.certificateField,
		certificates: form.certificates,
		onCertificatesChange: form.onCertificatesChange,
		labels
	})
] });
const buildSamlConfigurationPayload = async ({ mode, metadataUrl, metadataFile, manual }) => {
	if (mode === "metadataUrl") {
		if (!metadataUrl) throw new Error("metadataUrl values missing for mode \"metadataUrl\"");
		return { idpMetadataUrl: metadataUrl.value.trim() };
	}
	if (mode === "metadataFile") {
		if (!metadataFile?.file) throw new Error("metadataFile is missing for mode \"metadataFile\"");
		return { idpMetadata: await metadataFile.file.text() };
	}
	if (!manual) throw new Error("manual values missing for mode \"manual\"");
	const payload = {
		idpSsoUrl: manual.signOnUrl.trim(),
		idpEntityId: manual.issuer.trim()
	};
	if (haveCertificatesChanged(manual.certificates, manual.initialCertificates)) payload.idpCertificates = toIdpCertificatesParam(manual.certificates);
	return payload;
};
const applySamlSubmitError = (err, card, primaryField, additionalFields = []) => {
	handleError(err, [primaryField, ...additionalFields], card.setError);
	if (isClerkAPIResponseError(err)) {
		const unscopedSamlError = err.errors.find((e) => e.code?.startsWith("saml_") && !e.meta?.paramName);
		if (unscopedSamlError) {
			primaryField.setError(unscopedSamlError);
			card.setError(void 0);
		}
	}
};
const FileUploadField = ({ field, file, onFileChange, existingFilePresent, labels, accept }) => {
	const { t } = useLocalizations();
	const inputRef = React.useRef(null);
	return /* @__PURE__ */ jsx(Box, { children: /* @__PURE__ */ jsxs(Field.Root, {
		...field.props,
		children: [/* @__PURE__ */ jsxs(Col, {
			gap: 2,
			children: [
				/* @__PURE__ */ jsx(Field.LabelRow, { children: /* @__PURE__ */ jsx(Field.Label, {}) }),
				/* @__PURE__ */ jsx("input", {
					ref: inputRef,
					type: "file",
					accept,
					multiple: false,
					style: { display: "none" },
					onChange: (e) => {
						onFileChange(e.target.files?.[0] ?? null);
						field.clearFeedback();
					}
				}),
				file === null ? /* @__PURE__ */ jsxs(Flex, {
					align: "center",
					gap: 2,
					sx: {
						alignSelf: "flex-start",
						flexWrap: "wrap"
					},
					children: [existingFilePresent && /* @__PURE__ */ jsx(Badge, {
						elementDescriptor: descriptors.configureSSOCertificateFileBadge,
						localizationKey: labels.fileUploaded
					}), /* @__PURE__ */ jsxs(Button, {
						elementDescriptor: descriptors.configureSSOCertificateUploadButton,
						size: "xs",
						variant: "outline",
						onClick: () => inputRef.current?.click(),
						children: [/* @__PURE__ */ jsx(Icon, {
							icon: SvgArrowUpTray,
							size: "sm",
							colorScheme: "neutral",
							sx: (theme) => ({ marginInlineEnd: theme.space.$1 })
						}), /* @__PURE__ */ jsx(Text, {
							as: "span",
							localizationKey: existingFilePresent ? labels.replaceFile : labels.uploadFile
						})]
					})]
				}) : /* @__PURE__ */ jsxs(Flex, {
					align: "center",
					gap: 2,
					sx: (theme) => ({
						paddingTop: theme.space.$1,
						paddingBottom: theme.space.$1
					}),
					children: [/* @__PURE__ */ jsx(Text, {
						elementDescriptor: descriptors.configureSSOCertificateFileName,
						as: "span",
						colorScheme: "secondary",
						variant: "buttonSmall",
						children: file.name
					}), /* @__PURE__ */ jsx(Button, {
						elementDescriptor: descriptors.configureSSOCertificateRemoveButton,
						variant: "ghost",
						colorScheme: "neutral",
						"aria-label": t(labels.removeFile),
						onClick: () => {
							onFileChange(null);
							field.clearFeedback();
							if (inputRef.current) inputRef.current.value = "";
						},
						sx: (theme) => ({ padding: theme.space.$1 }),
						children: /* @__PURE__ */ jsx(Icon, {
							icon: SvgClose,
							size: "xs"
						})
					})]
				})
			]
		}), /* @__PURE__ */ jsx(Field.Feedback, {})]
	}) });
};
const CERTIFICATE_FILE_TYPES = ".pem,.key,.crt,.cer,.cert";
const CertificateListField = ({ field, certificates, onCertificatesChange, labels }) => {
	const { t } = useLocalizations();
	const inputRef = React.useRef(null);
	const canRemove = certificates.length > 1;
	const canAdd = certificates.length < 5;
	const onFileSelected = async (file) => {
		if (inputRef.current) inputRef.current.value = "";
		if (!file) return;
		let text;
		try {
			text = await file.text();
		} catch {
			field.setError(t(localizationKeys("configureSSO.signingCertificates.fileUnreadable")));
			return;
		}
		const bodies = parseCertificateFile(text);
		if (!areCertificateBodies(bodies)) {
			field.setError(t(localizationKeys("configureSSO.signingCertificates.notACertificate")));
			return;
		}
		field.clearFeedback();
		onCertificatesChange((current) => addCertificates(current, bodies));
	};
	return /* @__PURE__ */ jsx(Box, { children: /* @__PURE__ */ jsxs(Field.Root, {
		...field.props,
		children: [/* @__PURE__ */ jsxs(Col, {
			gap: 2,
			children: [
				/* @__PURE__ */ jsx(Field.LabelRow, { children: /* @__PURE__ */ jsx(Field.Label, {}) }),
				/* @__PURE__ */ jsx("input", {
					ref: inputRef,
					type: "file",
					accept: CERTIFICATE_FILE_TYPES,
					multiple: false,
					style: { display: "none" },
					onChange: (e) => void onFileSelected(e.target.files?.[0] ?? null)
				}),
				certificates.length > 0 && /* @__PURE__ */ jsx(Col, {
					elementDescriptor: descriptors.configureSSOCertificateList,
					gap: 2,
					children: certificates.map((entry, index) => /* @__PURE__ */ jsxs(Flex, {
						elementDescriptor: descriptors.configureSSOCertificateListItem,
						align: "center",
						gap: 2,
						sx: (theme) => ({
							padding: theme.space.$2,
							borderRadius: theme.radii.$md,
							borderWidth: theme.borderWidths.$normal,
							borderStyle: theme.borderStyles.$solid,
							borderColor: theme.colors.$borderAlpha100
						}),
						children: [/* @__PURE__ */ jsxs(Col, {
							gap: 1,
							sx: {
								minWidth: 0,
								flex: 1
							},
							children: [/* @__PURE__ */ jsxs(Flex, {
								align: "center",
								gap: 2,
								sx: { minWidth: 0 },
								children: [/* @__PURE__ */ jsx(Text, {
									elementDescriptor: descriptors.configureSSOCertificateListItemBody,
									as: "span",
									colorScheme: "secondary",
									variant: "buttonSmall",
									sx: {
										fontFamily: "monospace",
										overflow: "hidden",
										textOverflow: "ellipsis",
										whiteSpace: "nowrap"
									},
									children: entry.certificate
								}), index === 0 && /* @__PURE__ */ jsxs(Tooltip.Root, { children: [/* @__PURE__ */ jsx(Tooltip.Trigger, { children: /* @__PURE__ */ jsx(Span, {
									tabIndex: 0,
									sx: (theme) => ({
										display: "inline-flex",
										borderRadius: theme.radii.$sm
									}),
									children: /* @__PURE__ */ jsx(Badge, {
										elementDescriptor: descriptors.configureSSOCertificatePrimaryBadge,
										localizationKey: localizationKeys("configureSSO.signingCertificates.primary")
									})
								}) }), /* @__PURE__ */ jsx(Tooltip.Content, { text: localizationKeys("configureSSO.signingCertificates.primaryTooltip") })] })]
							}), /* @__PURE__ */ jsx(CertificateExpiry, { entry })]
						}), /* @__PURE__ */ jsx(Button, {
							elementDescriptor: descriptors.configureSSOCertificateListItemRemoveButton,
							variant: "ghost",
							colorScheme: "neutral",
							"aria-label": t(localizationKeys("configureSSO.signingCertificates.removeCertificate")),
							isDisabled: !canRemove,
							onClick: () => {
								field.clearFeedback();
								onCertificatesChange((current) => removeCertificate(current, entry.certificate));
							},
							sx: (theme) => ({ padding: theme.space.$1 }),
							children: /* @__PURE__ */ jsx(Icon, {
								icon: SvgClose,
								size: "xs"
							})
						})]
					}, entry.certificate))
				}),
				/* @__PURE__ */ jsxs(Button, {
					elementDescriptor: descriptors.configureSSOCertificateUploadButton,
					size: "xs",
					variant: "outline",
					onClick: () => inputRef.current?.click(),
					isDisabled: !canAdd,
					sx: { alignSelf: "flex-start" },
					children: [/* @__PURE__ */ jsx(Icon, {
						icon: SvgArrowUpTray,
						size: "sm",
						colorScheme: "neutral",
						sx: (theme) => ({ marginInlineEnd: theme.space.$1 })
					}), /* @__PURE__ */ jsx(Text, {
						as: "span",
						localizationKey: certificates.length > 0 ? localizationKeys("configureSSO.signingCertificates.addCertificate") : labels.uploadFile
					})]
				})
			]
		}), /* @__PURE__ */ jsx(Field.Feedback, {})]
	}) });
};
const CertificateExpiry = ({ entry }) => {
	const status = getIdpCertificateStatus(entry);
	const showsAlert = status === "expired" || status === "expiring";
	const colorScheme = status === "expired" ? "danger" : status === "expiring" ? "warning" : "secondary";
	return /* @__PURE__ */ jsxs(Flex, {
		elementDescriptor: descriptors.configureSSOCertificateListItemExpiry,
		align: "center",
		gap: 1,
		children: [showsAlert && /* @__PURE__ */ jsx(Icon, {
			icon: SvgExclamationTriangle,
			size: "sm",
			colorScheme: status === "expired" ? "danger" : "warning"
		}), /* @__PURE__ */ jsx(Text, {
			as: "span",
			colorScheme,
			variant: "caption",
			localizationKey: entry.expiresAt === null ? localizationKeys("configureSSO.signingCertificates.expiryAfterSave") : localizationKeys(status === "expired" ? "configureSSO.signingCertificates.expired" : "configureSSO.signingCertificates.expires", { date: formatDate(new Date(entry.expiresAt)) })
		})]
	});
};

//#endregion
export { IdentityProviderConfigurationForm, applySamlSubmitError, buildSamlConfigurationPayload };
//# sourceMappingURL=IdentityProviderConfigurationForm.js.map