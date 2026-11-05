import SvgInformationCircle from "../../icons/information-circle.js";
import { useEnvironment } from "../../contexts/EnvironmentContext.js";
import { localizationKeys } from "../../localization/localizationKeys.js";
import { useLocalizations } from "../../localization/makeLocalizable.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Badge, Button, Col, Flex, Icon, Text } from "../../customizables/index.js";
import { CardStateProvider, useCardState } from "../../elements/contexts/index.js";
import { Card } from "../../elements/Card/index.js";
import { ProfileSection } from "../../elements/Section.js";
import { ThreeDotsMenu } from "../../elements/ThreeDotsMenu.js";
import { handleError } from "../../utils/errorHandler.js";
import { useFetchRoles, useLocalizeCustomRoles } from "../../hooks/useFetchRoles.js";
import { Tooltip } from "../../elements/Tooltip.js";
import { ResetConnectionDialog } from "../ConfigureSSO/ResetConnectionDialog.js";
import { providerLabel, toProviderCard } from "../ConfigureSSO/domain/providers.js";
import { useOrganizationEnterpriseConnectionStatus } from "../ConfigureSSO/hooks/useOrganizationEnterpriseConnectionStatus.js";
import { EnterpriseConnectionIcon } from "./EnterpriseConnectionIcon.js";
import { STATUS_BADGES } from "./enterpriseConnectionStatusBadges.js";
import { useState } from "react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/SecuritySsoSection.tsx
const SecuritySsoSection = ({ enterpriseConnections, enterpriseConnectionMutations, organizationName, contentRef, onConfigure, onOpenConnection }) => {
	return /* @__PURE__ */ jsx(ProfileSection.Root, {
		title: localizationKeys("organizationProfile.securityPage.ssoSection.title"),
		id: "sso",
		centered: false,
		badge: /* @__PURE__ */ jsx(SsoInfoTooltip, {}),
		children: /* @__PURE__ */ jsxs(Col, {
			gap: 4,
			children: [enterpriseConnections.length > 0 && /* @__PURE__ */ jsx(ProfileSection.ItemList, {
				id: "sso",
				children: enterpriseConnections.map((connection) => /* @__PURE__ */ jsx(CardStateProvider, { children: /* @__PURE__ */ jsx(ConnectionRow, {
					connection,
					enterpriseConnectionMutations,
					organizationName,
					contentRef,
					onConfigure,
					onOpenConnection
				}) }, connection.id))
			}), /* @__PURE__ */ jsxs(Col, {
				sx: { width: "100%" },
				children: [onConfigure && /* @__PURE__ */ jsx(ProfileSection.ArrowButton, {
					id: "sso",
					elementDescriptor: [descriptors.profileSectionPrimaryButton, descriptors.organizationProfileSecuritySsoConfigureButton],
					localizationKey: enterpriseConnections.length === 0 ? localizationKeys("organizationProfile.securityPage.ssoSection.primaryButton__configure") : localizationKeys("organizationProfile.securityPage.ssoSection.primaryButton__addConnection"),
					onClick: () => onConfigure({ kind: "new" }, true)
				}), /* @__PURE__ */ jsx(SsoDescription, { sx: onConfigure ? (t) => ({ paddingInlineStart: `calc(${t.space.$2x5} + ${t.sizes.$4} + ${t.space.$2})` }) : void 0 })]
			})]
		})
	});
};
const ConnectionRow = ({ connection, enterpriseConnectionMutations: { setConnectionActive, deleteConnection }, organizationName, contentRef, onConfigure, onOpenConnection }) => {
	const card = useCardState();
	const [isRemoveDialogOpen, setIsRemoveDialogOpen] = useState(false);
	const { status } = useOrganizationEnterpriseConnectionStatus(connection, { probe: false });
	const badge = STATUS_BADGES[status];
	const label = providerLabel(toProviderCard(connection.provider));
	const onSetActive = async (active) => {
		if (card.isLoading) return;
		card.setError(void 0);
		card.setLoading();
		try {
			await setConnectionActive(connection.id, active);
		} catch (err) {
			handleError(err, [], card.setError);
		} finally {
			card.setIdle();
		}
	};
	const actions = onConfigure && onOpenConnection ? [
		{
			label: localizationKeys("organizationProfile.securityPage.ssoSection.menuAction__edit"),
			onClick: () => onOpenConnection(connection.id)
		},
		...status === "in_progress" ? [{
			label: localizationKeys("organizationProfile.securityPage.ssoSection.menuAction__continue"),
			onClick: () => onConfigure({
				kind: "existing",
				id: connection.id
			})
		}] : [],
		...status === "active" ? [{
			label: localizationKeys("organizationProfile.securityPage.ssoSection.menuAction__deactivate"),
			isDisabled: card.isLoading,
			onClick: () => void onSetActive(false)
		}] : [],
		...status === "inactive" ? [{
			label: localizationKeys("organizationProfile.securityPage.ssoSection.menuAction__activate"),
			isDisabled: card.isLoading,
			onClick: () => void onSetActive(true)
		}] : [],
		{
			label: localizationKeys("organizationProfile.securityPage.ssoSection.menuAction__remove"),
			isDestructive: true,
			onClick: () => setIsRemoveDialogOpen(true)
		}
	] : void 0;
	return /* @__PURE__ */ jsxs(Col, {
		elementDescriptor: descriptors.organizationProfileSecuritySsoConnectionRow,
		gap: 2,
		sx: { width: "100%" },
		children: [
			/* @__PURE__ */ jsxs(ProfileSection.Item, {
				id: "sso",
				children: [/* @__PURE__ */ jsxs(Flex, {
					align: "center",
					sx: (t) => ({
						minWidth: 0,
						flex: 1,
						gap: t.space.$3
					}),
					children: [/* @__PURE__ */ jsx(Flex, {
						align: "center",
						justify: "center",
						sx: (t) => ({
							flexShrink: 0,
							width: t.sizes.$10,
							height: t.sizes.$10,
							borderRadius: t.radii.$md,
							borderWidth: t.borderWidths.$normal,
							borderStyle: t.borderStyles.$solid,
							borderColor: t.colors.$borderAlpha150,
							backgroundColor: t.colors.$colorBackground
						}),
						children: /* @__PURE__ */ jsx(EnterpriseConnectionIcon, {
							connection,
							size: "$6"
						})
					}), /* @__PURE__ */ jsxs(Col, {
						sx: { minWidth: 0 },
						children: [/* @__PURE__ */ jsx(Text, {
							as: "span",
							children: connection.name
						}), connection.domains.length > 0 ? /* @__PURE__ */ jsx(Text, {
							as: "span",
							colorScheme: "secondary",
							variant: "caption",
							sx: { overflowWrap: "anywhere" },
							children: connection.domains.join(", ")
						}) : label && /* @__PURE__ */ jsx(Text, {
							as: "span",
							colorScheme: "secondary",
							variant: "caption",
							localizationKey: label
						})]
					})]
				}), /* @__PURE__ */ jsxs(Flex, {
					align: "center",
					sx: (t) => ({ gap: t.space.$2 }),
					children: [/* @__PURE__ */ jsx(Badge, {
						elementDescriptor: descriptors.organizationProfileSecuritySsoBadge,
						elementId: descriptors.organizationProfileSecuritySsoBadge.setId(badge.id),
						colorScheme: badge.colorScheme,
						localizationKey: badge.label
					}), actions && /* @__PURE__ */ jsx(ThreeDotsMenu, {
						elementId: "sso",
						actions
					})]
				})]
			}),
			/* @__PURE__ */ jsx(Card.Alert, { children: card.error }),
			actions && /* @__PURE__ */ jsx(ResetConnectionDialog, {
				isOpen: isRemoveDialogOpen,
				onClose: () => setIsRemoveDialogOpen(false),
				confirmationValue: organizationName,
				title: localizationKeys("organizationProfile.securityPage.removeDialog.title"),
				subtitle: localizationKeys("organizationProfile.securityPage.removeDialog.subtitle", { name: connection.name }),
				confirmButtonLabel: localizationKeys("organizationProfile.securityPage.removeDialog.confirmButton"),
				onDelete: () => deleteConnection(connection.id),
				contentRef
			})
		]
	});
};
const SsoDescription = ({ sx }) => /* @__PURE__ */ jsx(Text, {
	as: "p",
	elementDescriptor: descriptors.organizationProfileSecuritySsoDescription,
	colorScheme: "secondary",
	sx,
	localizationKey: localizationKeys("organizationProfile.securityPage.ssoSection.descriptionLine1")
});
const SsoInfoTooltip = () => {
	const roleName = useEnrollmentRoleName();
	const { t } = useLocalizations();
	return /* @__PURE__ */ jsxs(Tooltip.Root, { children: [/* @__PURE__ */ jsx(Tooltip.Trigger, { children: /* @__PURE__ */ jsx(Button, {
		variant: "unstyled",
		"aria-label": t(localizationKeys("organizationProfile.securityPage.ssoSection.tooltipLabel")),
		sx: (t) => ({
			display: "inline-flex",
			alignItems: "center",
			padding: 0,
			height: "fit-content",
			borderRadius: t.radii.$sm,
			color: t.colors.$colorMutedForeground
		}),
		children: /* @__PURE__ */ jsx(Icon, {
			icon: SvgInformationCircle,
			"aria-hidden": true,
			sx: (t) => ({
				width: t.sizes.$4,
				height: t.sizes.$4
			})
		})
	}) }), /* @__PURE__ */ jsx(Tooltip.Content, { text: roleName ? localizationKeys("organizationProfile.securityPage.ssoSection.tooltip", { role: roleName }) : localizationKeys("organizationProfile.securityPage.ssoSection.tooltip__noRole") })] });
};
/**
* The display name of the role SSO-enrolled members are assigned — the environment's
* default member role, name-mapped when the roles list is readable.
*/
const useEnrollmentRoleName = () => {
	const { organizationSettings } = useEnvironment();
	const { options } = useFetchRoles();
	const { localizeCustomRole } = useLocalizeCustomRoles();
	let roleKey = organizationSettings.domains.defaultRole ?? void 0;
	if (!roleKey && options?.length === 1) roleKey = options[0].value;
	if (!roleKey) return;
	return localizeCustomRole(roleKey) || options?.find((option) => option.value === roleKey)?.label || humanizeRoleKey(roleKey);
};
/** `org:billing_admin` → `billing admin`. */
const humanizeRoleKey = (roleKey) => {
	return (roleKey.split(":").pop() ?? roleKey).replace(/[_-]+/g, " ").trim() || roleKey;
};

//#endregion
export { SecuritySsoSection };
//# sourceMappingURL=SecuritySsoSection.js.map