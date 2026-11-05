import SvgCheckmark from "../../../icons/checkmark.js";
import SvgChevronDown from "../../../icons/chevron-down.js";
import SvgClipboard from "../../../icons/clipboard.js";
import SvgExclamationTriangle from "../../../icons/exclamation-triangle.js";
import { localizationKeys } from "../../../localization/localizationKeys.js";
import { useLocalizations } from "../../../localization/makeLocalizable.js";
import { descriptors } from "../../../customizables/elementDescriptors.js";
import { Badge, Button, Col, Flex, Icon, Input, Spinner, Text } from "../../../customizables/index.js";
import { useCardState } from "../../../elements/contexts/index.js";
import { Alert } from "../../../elements/Alert.js";
import { Collapsible } from "../../../elements/Collapsible.js";
import { handleError } from "../../../utils/errorHandler.js";
import { ClipboardInput } from "../../../elements/ClipboardInput.js";
import { useWizard } from "../../ConfigureSSO/elements/Wizard/WizardContext.js";
import { Step } from "../../ConfigureSSO/elements/Step.js";
import { useConfigureDirectorySync } from "../ConfigureDirectorySyncContext.js";
import { GoogleCredentialsForm, useGoogleCredentialsState } from "../GoogleCredentialsForm.js";
import { useEffect, useRef, useState } from "react";
import { Fragment as Fragment$1, jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/ConfigureDirectorySync/steps/ConfigureStep.tsx
const FieldLabel = ({ id, localizationKey }) => /* @__PURE__ */ jsx(Text, {
	elementDescriptor: descriptors.configureDirectorySyncFieldLabel,
	elementId: descriptors.configureDirectorySyncFieldLabel.setId(id),
	as: "span",
	localizationKey,
	sx: (t) => ({
		fontSize: t.fontSizes.$sm,
		fontWeight: t.fontWeights.$medium
	})
});
const ConfigureStep = () => {
	const { goNext } = useWizard();
	const { connection, providerMeta, directory, createDirectory, revealedToken, rotateToken } = useConfigureDirectorySync();
	const { t } = useLocalizations();
	const card = useCardState();
	const [isInstructionsOpen, setIsInstructionsOpen] = useState(false);
	const isPull = providerMeta?.mode === "pull";
	const credentials = useGoogleCredentialsState();
	const canProvision = Boolean(connection);
	const domains = connection?.domains ?? [];
	const instructions = providerMeta?.instructions ?? [];
	const run = async (action) => {
		if (card.isLoading) return;
		card.setError(void 0);
		card.setLoading();
		try {
			await action();
		} catch (err) {
			handleError(err, [], card.setError);
		} finally {
			card.setIdle();
		}
	};
	const submitCredentialsAndContinue = async () => {
		await credentials.submit();
		goNext();
	};
	const hasAttemptedCreate = useRef(false);
	useEffect(() => {
		if (!canProvision || directory !== null || hasAttemptedCreate.current) return;
		hasAttemptedCreate.current = true;
		run(createDirectory);
	}, [canProvision, directory]);
	return /* @__PURE__ */ jsxs(Fragment$1, { children: [
		/* @__PURE__ */ jsx(Step.Header, {
			title: localizationKeys("configureDirectorySync.configureStep.title"),
			description: localizationKeys("configureDirectorySync.configureStep.subtitle")
		}),
		/* @__PURE__ */ jsx(Step.Body, { children: /* @__PURE__ */ jsxs(Step.Section, {
			sx: (t) => ({ gap: t.space.$5 }),
			children: [
				!connection ? /* @__PURE__ */ jsx(Alert, {
					variant: "warning",
					title: localizationKeys("configureDirectorySync.configureStep.error__ssoRequired.title"),
					subtitle: localizationKeys("configureDirectorySync.configureStep.error__ssoRequired.subtitle")
				}) : /* @__PURE__ */ jsxs(Fragment$1, { children: [
					/* @__PURE__ */ jsxs(Col, {
						elementDescriptor: descriptors.configureDirectorySyncConnectionCard,
						sx: (t) => ({
							borderRadius: t.radii.$md,
							borderWidth: t.borderWidths.$normal,
							borderStyle: t.borderStyles.$solid,
							borderColor: t.colors.$borderAlpha150,
							overflow: "hidden"
						}),
						children: [/* @__PURE__ */ jsxs(Col, {
							sx: (t) => ({
								gap: t.space.$2,
								padding: t.space.$4
							}),
							children: [/* @__PURE__ */ jsx(Text, {
								elementDescriptor: descriptors.configureDirectorySyncConnectionCardName,
								as: "span",
								localizationKey: providerMeta?.name,
								sx: (t) => ({ fontWeight: t.fontWeights.$medium }),
								children: connection.name
							}), domains.length > 0 && /* @__PURE__ */ jsxs(Flex, {
								elementDescriptor: descriptors.configureDirectorySyncConnectionCardDomains,
								align: "center",
								wrap: "wrap",
								sx: (t) => ({ gap: t.space.$1x5 }),
								children: [/* @__PURE__ */ jsx(Text, {
									as: "span",
									colorScheme: "secondary",
									localizationKey: localizationKeys("configureDirectorySync.configureStep.domainsLabel"),
									sx: (t) => ({ fontSize: t.fontSizes.$sm })
								}), domains.map((domain) => /* @__PURE__ */ jsx(Badge, {
									elementDescriptor: descriptors.configureDirectorySyncConnectionCardDomainBadge,
									children: domain
								}, domain))]
							})]
						}), instructions.length > 0 && /* @__PURE__ */ jsxs(Col, {
							sx: (t) => ({
								backgroundColor: t.colors.$neutralAlpha25,
								borderTopWidth: t.borderWidths.$normal,
								borderTopStyle: t.borderStyles.$solid,
								borderTopColor: t.colors.$borderAlpha100
							}),
							children: [/* @__PURE__ */ jsxs(Button, {
								elementDescriptor: descriptors.configureDirectorySyncInstructionsToggle,
								variant: "ghost",
								colorScheme: "secondary",
								size: "sm",
								"aria-expanded": isInstructionsOpen,
								onClick: () => setIsInstructionsOpen((open) => !open),
								sx: (t) => ({
									justifyContent: "start",
									gap: t.space.$1x5,
									padding: `${t.space.$3} ${t.space.$4}`,
									borderRadius: 0,
									color: t.colors.$colorForeground,
									"&:hover": { color: t.colors.$colorForeground }
								}),
								children: [/* @__PURE__ */ jsx(Text, {
									as: "span",
									localizationKey: localizationKeys("configureDirectorySync.configureStep.instructions.actionLabel__toggle"),
									sx: (t) => ({
										fontSize: t.fontSizes.$sm,
										fontWeight: t.fontWeights.$medium
									})
								}), /* @__PURE__ */ jsx(Icon, {
									icon: SvgChevronDown,
									size: "sm",
									sx: (t) => ({
										transform: isInstructionsOpen ? "rotate(180deg)" : "none",
										transition: `transform ${t.transitionDuration.$fast}`
									})
								})]
							}), /* @__PURE__ */ jsx(Collapsible, {
								open: isInstructionsOpen,
								children: /* @__PURE__ */ jsx(Col, {
									elementDescriptor: descriptors.configureDirectorySyncInstructionsList,
									as: "ol",
									sx: (t) => ({
										gap: t.space.$1x5,
										padding: t.space.$4,
										paddingInlineStart: t.space.$8,
										listStyle: "decimal"
									}),
									children: instructions.map((instruction) => /* @__PURE__ */ jsx(Text, {
										elementDescriptor: descriptors.configureDirectorySyncInstructionsListItem,
										as: "li",
										colorScheme: "secondary",
										localizationKey: instruction,
										sx: (t) => ({ fontSize: t.fontSizes.$sm })
									}, instruction.key))
								})
							})]
						})]
					}),
					!connection.active && /* @__PURE__ */ jsx(Alert, {
						variant: "warning",
						title: localizationKeys("configureDirectorySync.configureStep.warning__ssoInactive")
					}),
					directory ? isPull ? /* @__PURE__ */ jsx(GoogleCredentialsForm, { state: credentials }) : /* @__PURE__ */ jsxs(Fragment$1, { children: [/* @__PURE__ */ jsxs(Col, {
						sx: (t) => ({ gap: t.space.$1x5 }),
						children: [/* @__PURE__ */ jsx(FieldLabel, {
							id: "endpointUrl",
							localizationKey: localizationKeys("configureDirectorySync.configureStep.formFieldLabel__endpointUrl")
						}), /* @__PURE__ */ jsx(ClipboardInput, {
							elementDescriptor: descriptors.configureDirectorySyncEndpointUrlInput,
							value: directory.endpointUrl,
							readOnly: true,
							copyIcon: SvgClipboard,
							copiedIcon: SvgCheckmark
						})]
					}), /* @__PURE__ */ jsxs(Col, {
						sx: (t) => ({ gap: t.space.$1x5 }),
						children: [
							/* @__PURE__ */ jsx(FieldLabel, {
								id: "token",
								localizationKey: localizationKeys("configureDirectorySync.configureStep.formFieldLabel__token")
							}),
							/* @__PURE__ */ jsxs(Flex, {
								align: "center",
								sx: (t) => ({ gap: t.space.$2 }),
								children: [revealedToken ? /* @__PURE__ */ jsx(ClipboardInput, {
									elementDescriptor: descriptors.configureDirectorySyncTokenInput,
									value: revealedToken,
									readOnly: true,
									copyIcon: SvgClipboard,
									copiedIcon: SvgCheckmark,
									sx: { flex: 1 }
								}) : /* @__PURE__ */ jsx(Input, {
									elementDescriptor: descriptors.configureDirectorySyncTokenInput,
									value: "",
									readOnly: true,
									placeholder: t(localizationKeys("configureDirectorySync.configureStep.formFieldInputPlaceholder__token")),
									sx: { flex: 1 }
								}), /* @__PURE__ */ jsx(Button, {
									elementDescriptor: descriptors.configureDirectorySyncGenerateTokenButton,
									variant: "outline",
									size: "sm",
									isLoading: card.isLoading,
									onClick: () => void run(rotateToken),
									localizationKey: localizationKeys("configureDirectorySync.configureStep.actionLabel__generateToken"),
									sx: { flexShrink: 0 }
								})]
							}),
							/* @__PURE__ */ jsxs(Flex, {
								elementDescriptor: descriptors.configureDirectorySyncTokenNotice,
								align: "center",
								sx: (t) => ({ gap: t.space.$1x5 }),
								children: [/* @__PURE__ */ jsx(Icon, {
									icon: SvgExclamationTriangle,
									size: "sm",
									colorScheme: "neutral"
								}), /* @__PURE__ */ jsx(Text, {
									as: "span",
									colorScheme: "secondary",
									localizationKey: localizationKeys("configureDirectorySync.configureStep.notice__tokenShownOnce"),
									sx: (t) => ({ fontSize: t.fontSizes.$sm })
								})]
							})
						]
					})] }) : canProvision && !card.error && /* @__PURE__ */ jsx(Flex, {
						align: "center",
						justify: "center",
						sx: (t) => ({ paddingBlock: t.space.$5 }),
						children: /* @__PURE__ */ jsx(Spinner, {
							size: "xs",
							colorScheme: "neutral",
							elementDescriptor: descriptors.spinner
						})
					})
				] }),
				card.error && /* @__PURE__ */ jsx(Alert, {
					variant: "danger",
					title: card.error
				}),
				!directory && canProvision && card.error && /* @__PURE__ */ jsx(Button, {
					elementDescriptor: descriptors.configureDirectorySyncRetryButton,
					variant: "outline",
					size: "sm",
					onClick: () => void run(createDirectory),
					localizationKey: localizationKeys("configureDirectorySync.configureStep.actionLabel__retry"),
					sx: { alignSelf: "start" }
				})
			]
		}) }),
		/* @__PURE__ */ jsx(Step.Footer, { children: /* @__PURE__ */ jsx(Step.Footer.Continue, {
			onClick: isPull ? () => void run(submitCredentialsAndContinue) : () => goNext(),
			isDisabled: !directory || isPull && !credentials.canContinue,
			isLoading: isPull && card.isLoading
		}) })
	] });
};

//#endregion
export { ConfigureStep };
//# sourceMappingURL=ConfigureStep.js.map