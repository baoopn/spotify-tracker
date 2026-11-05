import { localizationKeys } from "../../../localization/localizationKeys.js";
import { useLocalizations } from "../../../localization/makeLocalizable.js";
import { common } from "../../../styledSystem/common.js";
import { mqu } from "../../../styledSystem/breakpoints.js";
import { descriptors } from "../../../customizables/elementDescriptors.js";
import { Flow } from "../../../customizables/Flow.js";
import { Box, Col, Grid, RadioInput, Text } from "../../../customizables/index.js";
import { useCardState } from "../../../elements/contexts/index.js";
import { Alert } from "../../../elements/Alert.js";
import { getFieldError, getGlobalError, handleError } from "../../../utils/errorHandler.js";
import { ProviderIcon } from "../../../common/ProviderIcon.js";
import { useWizard } from "../elements/Wizard/WizardContext.js";
import { useConfigureSSO } from "../ConfigureSSOContext.js";
import { Step } from "../elements/Step.js";
import { ChangeProviderDialog } from "../ChangeProviderDialog.js";
import { PROVIDER_GROUPS, providerLabel, toProviderCard } from "../domain/providers.js";
import React from "react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";
import { iconImageUrl } from "@clerk/shared/constants";

//#region src/components/ConfigureSSO/steps/SelectProviderStep.tsx
const SelectProviderStep = () => {
	const { organizationEnterpriseConnection: c, enterpriseConnection, enterpriseConnectionMutations: { createConnection, changeProvider }, contentRef } = useConfigureSSO();
	const { goNext, goPrev, isFirstStep } = useWizard();
	const { t } = useLocalizations();
	const currentCard = c.provider ? toProviderCard(c.provider) : null;
	const [selected, setSelected] = React.useState(currentCard);
	const card = useCardState();
	const [isSubmitting, setIsSubmitting] = React.useState(false);
	const [isChangeDialogOpen, setIsChangeDialogOpen] = React.useState(false);
	const [changeFromProvider, setChangeFromProvider] = React.useState(null);
	const handleSelect = (next) => {
		setSelected(next);
	};
	const isChangingProvider = c.hasConnection && selected !== null && selected !== currentCard;
	const handleContinue = async () => {
		if (!selected) return;
		if (c.hasConnection && selected === currentCard) {
			goNext();
			return;
		}
		if (isChangingProvider) {
			setChangeFromProvider(currentCard);
			setIsChangeDialogOpen(true);
			return;
		}
		card.setError(void 0);
		setIsSubmitting(true);
		try {
			await createConnection(selected);
			goNext();
		} catch (err) {
			handleCreateError(err);
			setIsSubmitting(false);
		}
	};
	const handleCreateError = (err) => {
		handleError(err, [], card.setError);
		const fieldError = getFieldError(err);
		if (fieldError && !getGlobalError(err)) card.setError(fieldError);
	};
	const handleConfirmChangeProvider = async () => {
		if (!selected) return;
		card.setError(void 0);
		setIsSubmitting(true);
		try {
			if (enterpriseConnection) await changeProvider(enterpriseConnection.id, selected);
			else await createConnection(selected);
			goNext();
		} catch (err) {
			handleCreateError(err);
			setIsChangeDialogOpen(false);
			setChangeFromProvider(null);
			setIsSubmitting(false);
		}
	};
	const currentProviderLabel = changeFromProvider ? providerLabel(changeFromProvider) : void 0;
	const nextProviderLabel = selected ? providerLabel(selected) : void 0;
	return /* @__PURE__ */ jsx(Flow.Part, {
		part: "selectProvider",
		children: /* @__PURE__ */ jsxs(Step, {
			elementDescriptor: descriptors.configureSSOStep,
			elementId: descriptors.configureSSOStep.setId("select-provider"),
			children: [
				/* @__PURE__ */ jsx(Step.Header, {
					title: localizationKeys("configureSSO.selectProviderStep.title"),
					description: localizationKeys("configureSSO.selectProviderStep.subtitle")
				}),
				/* @__PURE__ */ jsx(Step.Body, { children: /* @__PURE__ */ jsxs(Step.Section, {
					sx: (theme) => ({ gap: theme.space.$5 }),
					children: [PROVIDER_GROUPS.map((group) => /* @__PURE__ */ jsxs(Col, {
						elementDescriptor: descriptors.configureSSOProviderGroup,
						elementId: descriptors.configureSSOProviderGroup.setId(group.id),
						gap: 3,
						children: [/* @__PURE__ */ jsx(Text, {
							elementDescriptor: descriptors.configureSSOProviderGroupLabel,
							elementId: descriptors.configureSSOProviderGroupLabel.setId(group.id),
							as: "label",
							variant: "subtitle",
							localizationKey: group.label
						}), /* @__PURE__ */ jsx(Grid, {
							role: "radiogroup",
							"aria-label": t(group.label),
							elementDescriptor: descriptors.configureSSOProviderGrid,
							gap: 3,
							sx: {
								gridTemplateColumns: "repeat(2, 1fr)",
								[mqu.sm]: { gridTemplateColumns: "1fr" }
							},
							children: group.options.map((option) => /* @__PURE__ */ jsx(ProviderCard, {
								name: "configure-sso-provider",
								value: option.id,
								iconId: option.iconId,
								label: option.label,
								checked: selected === option.id,
								onChange: () => handleSelect(option.id)
							}, option.id))
						})]
					}, group.id)), card.error && /* @__PURE__ */ jsx(Alert, {
						variant: "danger",
						title: card.error,
						sx: (t) => ({ margin: t.space.$3 })
					})]
				}) }),
				/* @__PURE__ */ jsxs(Step.Footer, { children: [/* @__PURE__ */ jsx(Step.Footer.Previous, {
					onClick: () => goPrev(),
					isDisabled: isFirstStep || isSubmitting
				}), /* @__PURE__ */ jsx(Step.Footer.Continue, {
					onClick: handleContinue,
					isLoading: isSubmitting && !isChangeDialogOpen,
					isDisabled: !selected || isSubmitting
				})] }),
				currentProviderLabel && nextProviderLabel ? /* @__PURE__ */ jsx(ChangeProviderDialog, {
					isOpen: isChangeDialogOpen,
					onClose: () => {
						setIsChangeDialogOpen(false);
						setChangeFromProvider(null);
					},
					onConfirm: () => {
						handleConfirmChangeProvider();
					},
					isSubmitting,
					nextProviderLabel,
					currentProviderLabel,
					connectionName: enterpriseConnection?.name ?? "",
					contentRef
				}) : null
			]
		})
	});
};
const ProviderCard = ({ name, value, iconId, label, checked, onChange }) => {
	const { t } = useLocalizations();
	const labelText = t(label);
	return /* @__PURE__ */ jsxs(Box, {
		as: "label",
		elementDescriptor: descriptors.configureSSOProviderCard,
		elementId: descriptors.configureSSOProviderCard.setId(value),
		isActive: checked,
		sx: (t) => ({
			display: "flex",
			flexDirection: "column",
			alignItems: "center",
			justifyContent: "center",
			gap: t.space.$2,
			height: t.sizes.$32,
			padding: t.space.$1x5,
			cursor: "pointer",
			position: "relative",
			...common.borderVariants(t).normal,
			"&:has(input:focus-visible)": {
				...common.focusRingStyles(t),
				borderColor: t.colors.$borderAlpha300
			},
			"&:hover": { backgroundColor: t.colors.$neutralAlpha50 },
			"&:has(input:checked)": { backgroundColor: t.colors.$neutralAlpha50 }
		}),
		children: [
			/* @__PURE__ */ jsx(RadioInput, {
				elementDescriptor: descriptors.configureSSOProviderCardRadio,
				elementId: descriptors.configureSSOProviderCardRadio.setId(value),
				name,
				value,
				checked,
				onChange,
				focusRing: false,
				sx: common.visuallyHidden()
			}),
			/* @__PURE__ */ jsx(ProviderIcon, {
				id: iconId,
				iconUrl: iconImageUrl(iconId),
				name: labelText,
				size: "$8",
				"aria-hidden": true,
				elementDescriptor: descriptors.configureSSOProviderCardIcon,
				elementId: descriptors.configureSSOProviderCardIcon.setId(value)
			}),
			/* @__PURE__ */ jsx(Text, {
				elementDescriptor: descriptors.configureSSOProviderCardLabel,
				elementId: descriptors.configureSSOProviderCardLabel.setId(value),
				as: "span",
				variant: "body",
				sx: (theme) => ({ color: theme.colors.$colorForeground }),
				children: labelText
			})
		]
	});
};

//#endregion
export { SelectProviderStep };
//# sourceMappingURL=SelectProviderStep.js.map