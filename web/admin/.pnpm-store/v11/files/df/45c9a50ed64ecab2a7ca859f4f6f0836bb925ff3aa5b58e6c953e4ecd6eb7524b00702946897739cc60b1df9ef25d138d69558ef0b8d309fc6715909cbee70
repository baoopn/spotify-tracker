import SvgExclamationTriangle from "../../icons/exclamation-triangle.js";
import SvgInformationCircle from "../../icons/information-circle.js";
import SvgUserPlus from "../../icons/user-plus.js";
import { useFetch } from "../../hooks/useFetch.js";
import { localizationKeys } from "../../localization/localizationKeys.js";
import { useLocalizations } from "../../localization/makeLocalizable.js";
import { mqu } from "../../styledSystem/breakpoints.js";
import { useFormControl } from "../../utils/useFormControl.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Badge, Button, Col, Flex, Icon, Td, Text } from "../../customizables/index.js";
import { useCardState, withCardStateProvider } from "../../elements/contexts/index.js";
import { Alert } from "../../elements/Alert.js";
import { Card } from "../../elements/Card/index.js";
import { Header } from "../../elements/Header.js";
import { ProfileCard } from "../../elements/ProfileCard/index.js";
import { Animated } from "../../elements/Animated.js";
import { ThreeDotsMenu } from "../../elements/ThreeDotsMenu.js";
import { handleError } from "../../utils/errorHandler.js";
import { Form } from "../../elements/Form.js";
import { FormButtons } from "../../elements/FormButtons.js";
import { FormContainer } from "../../elements/FormContainer.js";
import { Wizard, useWizard } from "../../common/Wizard.js";
import { useActionContext } from "../../elements/Action/ActionRoot.js";
import { Action } from "../../elements/Action/index.js";
import { UserPreview } from "../../elements/UserPreview.js";
import { SuccessPage } from "../../elements/SuccessPage.js";
import { useFetchRoles } from "../../hooks/useFetchRoles.js";
import { RoleSelect } from "./MemberListTable.js";
import { IconCircle } from "../../elements/IconCircle.js";
import { SearchInput } from "../../elements/SearchInput.js";
import { SegmentedControl } from "../../elements/SegmentedControl.js";
import { SecurityBackControl } from "./SecurityBackControl.js";
import { DataTable, DataTableRow } from "../../elements/DataTable.js";
import React, { useCallback, useMemo, useState } from "react";
import { __internal_useOrganizationSSOBypassAllowlist, useOrganization, useUser } from "@clerk/shared/react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OrganizationProfile/SSOBypassAllowlistPage.tsx
const MEMBER_LOOKUP_PAGE_SIZE = 10;
const ROLE_MEMBERS_PAGE_SIZE = 100;
const DOMAIN_NOT_SERVED = "sso_bypass_domain_not_served";
const NOT_A_MEMBER = "resource_not_found";
const findMemberByEmail = async (organization, email) => {
	const wanted = email.toLowerCase();
	let fetched = 0;
	for (let page = 1;; page++) {
		const { data, total_count } = await organization.getMemberships({
			query: email,
			pageSize: MEMBER_LOOKUP_PAGE_SIZE,
			initialPage: page
		});
		const match = data.find((membership) => membership.publicUserData?.identifier?.toLowerCase() === wanted);
		fetched += data.length;
		if (match || data.length === 0 || fetched >= total_count) return match;
	}
};
const useRoleMemberCounts = (roles, enabled) => {
	const { organization } = useOrganization();
	const roleKeys = (roles ?? []).map((role) => role.value);
	const fetchCounts = async ({ keys }) => {
		if (!organization) return {};
		const entries = await Promise.all(keys.map(async (role) => {
			const { total_count } = await organization.getMemberships({
				role: [role],
				pageSize: 1
			});
			return [role, total_count];
		}));
		return Object.fromEntries(entries);
	};
	const shouldFetch = enabled && Boolean(organization?.id) && roleKeys.length > 0;
	const { data } = useFetch(shouldFetch ? fetchCounts : void 0, {
		keys: roleKeys,
		orgId: organization?.id,
		enabled: shouldFetch
	});
	return data ?? {};
};
const collectUserIdsByRole = async (organization, role) => {
	const userIds = [];
	let fetched = 0;
	for (let page = 1;; page++) {
		const { data, total_count } = await organization.getMemberships({
			role: [role],
			pageSize: ROLE_MEMBERS_PAGE_SIZE,
			initialPage: page
		});
		fetched += data.length;
		data.forEach((membership) => {
			const userId = membership.publicUserData?.userId;
			if (userId) userIds.push(userId);
		});
		if (data.length === 0 || fetched >= total_count) return userIds;
	}
};
const sharedCode = (codes) => codes.length > 0 && codes.every((code) => code === codes[0]) ? codes[0] : null;
const skippedText = (skipped, code) => {
	const reason = code === DOMAIN_NOT_SERVED ? "domainNotServed" : code === NOT_A_MEMBER ? "notMember" : "unknown";
	return skipped === 1 ? localizationKeys(`organizationProfile.securityPage.ssoBypassPage.bulkResult.${reason}__one`) : localizationKeys(`organizationProfile.securityPage.ssoBypassPage.bulkResult.${reason}`, { count: String(skipped) });
};
const InlineMessage = (props) => /* @__PURE__ */ jsxs(Flex, {
	elementDescriptor: props.elementDescriptor,
	align: "center",
	gap: 2,
	children: [/* @__PURE__ */ jsx(Icon, {
		icon: props.icon,
		size: "sm",
		colorScheme: "neutral",
		sx: { flexShrink: 0 }
	}), /* @__PURE__ */ jsx(Text, {
		as: "span",
		colorScheme: "secondary",
		variant: "caption",
		...typeof props.text === "string" ? { children: props.text } : { localizationKey: props.text }
	})]
});
const matchesSearch = (entry, term) => {
	const { firstName, lastName, identifier, username } = entry.publicUserData;
	return [
		firstName,
		lastName,
		identifier,
		username
	].filter(Boolean).join(" ").toLowerCase().includes(term);
};
const SSOBypassAllowlistPage = withCardStateProvider(({ onBack }) => {
	const card = useCardState();
	const { t } = useLocalizations();
	const { user } = useUser();
	const { data, isLoading, error, addUser, addUsers, removeUser } = __internal_useOrganizationSSOBypassAllowlist();
	const [search, setSearch] = useState("");
	const term = search.trim().toLowerCase();
	const entries = useMemo(() => term ? (data ?? []).filter((entry) => matchesSearch(entry, term)) : data ?? [], [data, term]);
	const allowlistedUserIds = useMemo(() => new Set((data ?? []).map((entry) => entry.userId)), [data]);
	const handleRemove = (userId) => card.runAsync(() => removeUser(userId)).catch((err) => handleError(err, [], card.setError));
	return /* @__PURE__ */ jsx(ProfileCard.Page, { children: /* @__PURE__ */ jsx(Col, {
		elementDescriptor: [descriptors.page, descriptors.organizationProfileSecuritySsoBypassPage],
		sx: (t) => ({ gap: t.space.$8 }),
		children: /* @__PURE__ */ jsxs(Col, {
			elementDescriptor: descriptors.profilePage,
			elementId: descriptors.profilePage.setId("organizationSecurity"),
			gap: 4,
			children: [
				/* @__PURE__ */ jsxs(Col, {
					gap: 4,
					children: [/* @__PURE__ */ jsx(Flex, { children: /* @__PURE__ */ jsx(SecurityBackControl, { onClick: onBack }) }), /* @__PURE__ */ jsx(Header.Root, { children: /* @__PURE__ */ jsx(Header.Title, {
						localizationKey: localizationKeys("organizationProfile.securityPage.ssoBypassPage.title"),
						textVariant: "h2"
					}) })]
				}),
				/* @__PURE__ */ jsxs(Action.Root, {
					animate: false,
					children: [/* @__PURE__ */ jsx(Animated, {
						asChild: true,
						children: /* @__PURE__ */ jsxs(Flex, {
							justify: "between",
							gap: 2,
							sx: (t) => ({
								width: "100%",
								padding: `${t.space.$none} ${t.space.$1}`
							}),
							children: [/* @__PURE__ */ jsx(Flex, {
								sx: {
									width: "50%",
									[mqu.sm]: { width: "auto" }
								},
								children: /* @__PURE__ */ jsx(SearchInput, {
									value: search,
									"aria-label": t(localizationKeys("organizationProfile.securityPage.ssoBypassPage.action__search")),
									placeholder: t(localizationKeys("organizationProfile.securityPage.ssoBypassPage.action__search")),
									elementDescriptor: descriptors.organizationProfileSecuritySsoBypassSearchInput,
									leftIconElementDescriptor: descriptors.organizationProfileSecuritySsoBypassSearchInputIcon,
									onChange: (e) => setSearch(e.target.value),
									onClear: () => setSearch("")
								})
							}), /* @__PURE__ */ jsx(Action.Trigger, {
								value: "add",
								hideOnActive: false,
								children: /* @__PURE__ */ jsx(Button, {
									elementDescriptor: descriptors.organizationProfileSecuritySsoBypassAddButton,
									localizationKey: localizationKeys("organizationProfile.securityPage.ssoBypassPage.action__add")
								})
							})]
						})
					}), /* @__PURE__ */ jsx(Action.Open, {
						value: "add",
						children: /* @__PURE__ */ jsx(Flex, {
							sx: (t) => ({ padding: `${t.space.$none} ${t.space.$1} ${t.space.$6} ${t.space.$1}` }),
							children: /* @__PURE__ */ jsx(Action.Card, {
								sx: { width: "100%" },
								children: /* @__PURE__ */ jsx(AddMemberScreen, {
									allowlistedUserIds,
									addUser,
									addUsers
								})
							})
						})
					})]
				}),
				/* @__PURE__ */ jsx(Card.Alert, { children: card.error }),
				error ? /* @__PURE__ */ jsx(Alert, {
					variant: "danger",
					title: localizationKeys("organizationProfile.securityPage.ssoBypassSection.error__load"),
					subtitle: error.message
				}) : /* @__PURE__ */ jsx(DataTable, {
					page: 1,
					onPageChange: () => {},
					itemCount: entries.length,
					pageCount: 1,
					itemsPerPage: Math.max(entries.length, 1),
					isLoading,
					emptyStateLocalizationKey: term ? localizationKeys("organizationProfile.securityPage.ssoBypassPage.table.emptyState__search") : localizationKeys("organizationProfile.securityPage.ssoBypassPage.table.emptyState"),
					headers: [{ key: localizationKeys("organizationProfile.securityPage.ssoBypassPage.table.header__user") }, {
						key: localizationKeys("organizationProfile.securityPage.ssoBypassPage.table.header__actions"),
						align: "right"
					}],
					rows: entries.map((entry) => /* @__PURE__ */ jsx(AllowlistRow, {
						entry,
						isCurrentUser: user?.id === entry.userId,
						onRemove: () => void handleRemove(entry.userId)
					}, entry.userId))
				})
			]
		})
	}) });
});
const AllowlistRow = ({ entry, isCurrentUser, onRemove }) => {
	const card = useCardState();
	return /* @__PURE__ */ jsxs(DataTableRow, { children: [/* @__PURE__ */ jsx(Td, { children: /* @__PURE__ */ jsx(UserPreview, {
		sx: { maxWidth: "30ch" },
		user: entry.publicUserData,
		subtitle: entry.publicUserData.identifier,
		subtitleProps: { variant: "caption" },
		badge: isCurrentUser ? /* @__PURE__ */ jsx(Badge, { localizationKey: localizationKeys("badge__you") }) : void 0
	}) }), /* @__PURE__ */ jsx(Td, { children: /* @__PURE__ */ jsx(Flex, {
		justify: "end",
		children: /* @__PURE__ */ jsx(ThreeDotsMenu, {
			elementId: "ssoBypass",
			actions: [{
				label: localizationKeys("organizationProfile.securityPage.ssoBypassPage.table.menuAction__remove"),
				isDestructive: true,
				isDisabled: card.isLoading,
				onClick: onRemove
			}]
		})
	}) })] });
};
const addedText = (result) => {
	if (result?.mode === "email") return localizationKeys("organizationProfile.securityPage.ssoBypassPage.bulkResult.addedMember");
	return result?.added === 1 ? localizationKeys("organizationProfile.securityPage.ssoBypassPage.bulkResult.added__one") : localizationKeys("organizationProfile.securityPage.ssoBypassPage.bulkResult.added", { count: String(result?.added ?? 0) });
};
const AddMemberScreen = (props) => {
	const { close } = useActionContext();
	const wizard = useWizard();
	const [bulkResult, setBulkResult] = useState(null);
	return /* @__PURE__ */ jsxs(Wizard, {
		...wizard.props,
		children: [/* @__PURE__ */ jsx(AddMemberForm, {
			...props,
			onReset: close,
			onResult: (result) => {
				setBulkResult(result);
				wizard.nextStep();
			}
		}), /* @__PURE__ */ jsx(SuccessPage, {
			elementDescriptor: descriptors.organizationProfileSecuritySsoBypassBulkResult,
			title: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.title"),
			contents: /* @__PURE__ */ jsxs(Col, {
				gap: 4,
				children: [/* @__PURE__ */ jsxs(Flex, {
					direction: "col",
					center: true,
					gap: 4,
					children: [/* @__PURE__ */ jsx(IconCircle, { icon: SvgUserPlus }), /* @__PURE__ */ jsx(Text, {
						localizationKey: addedText(bulkResult),
						sx: { textAlign: "center" }
					})]
				}), bulkResult && bulkResult.skipped > 0 && /* @__PURE__ */ jsx(Alert, {
					variant: "warning",
					title: skippedText(bulkResult.skipped, bulkResult.skippedCode)
				})]
			}),
			onFinish: close
		})]
	});
};
const AddMemberForm = withCardStateProvider(({ allowlistedUserIds, addUser, addUsers, onResult, onReset }) => {
	const card = useCardState();
	const { t, translateError } = useLocalizations();
	const [mode, setMode] = useState("email");
	const [role, setRole] = useState("");
	const [failure, setFailure] = useState(null);
	const { organization } = useOrganization();
	const { options: roles } = useFetchRoles();
	const roleCounts = useRoleMemberCounts(roles, mode === "role");
	const emailField = useFormControl("emailAddress", "", {
		type: "email",
		label: localizationKeys("formFieldLabel__emailAddress"),
		placeholder: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.emailPlaceholder"),
		isRequired: true
	});
	const formatRoleLabel = useCallback((label, role) => {
		const count = roleCounts[role.value];
		return count === void 0 ? label : t(localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.roleOption", {
			role: label,
			count: String(count)
		}));
	}, [roleCounts, t]);
	const email = emailField.value.trim();
	const canSubmit = !card.isLoading && (mode === "email" ? email !== "" : Boolean(role));
	const changeMode = (next) => {
		setFailure(null);
		setMode(next);
	};
	const addByEmail = async () => {
		if (!organization) return;
		const userId = (await findMemberByEmail(organization, email))?.publicUserData?.userId;
		if (!userId) {
			setFailure(localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.error__memberNotFound"));
			return;
		}
		if (allowlistedUserIds.has(userId)) {
			setFailure(localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.error__alreadyAdded"));
			return;
		}
		await addUser({ userId });
		return {
			mode: "email",
			added: 1,
			skipped: 0,
			skippedCode: null
		};
	};
	const addByRole = async () => {
		if (!organization) return;
		const userIds = (await collectUserIdsByRole(organization, role)).filter((userId) => !allowlistedUserIds.has(userId));
		if (userIds.length === 0) {
			setFailure(localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.error__allAlreadyAdded"));
			return;
		}
		const result = await addUsers({ userIds });
		const added = result?.data.length ?? 0;
		const skipped = result?.errors ?? [];
		const skippedCode = sharedCode(skipped.map((error) => error.code));
		if (added === 0) {
			setFailure(skippedText(skipped.length, skippedCode));
			return;
		}
		return {
			mode: "role",
			added,
			skipped: skipped.length,
			skippedCode
		};
	};
	const onSubmit = async (e) => {
		e.preventDefault();
		if (!canSubmit) return;
		setFailure(null);
		try {
			const result = await card.runAsync(mode === "email" ? addByEmail : addByRole);
			if (result) onResult(result);
		} catch (err) {
			handleError(err, [emailField], (error) => setFailure(translateError(error)));
		}
	};
	return /* @__PURE__ */ jsx(FormContainer, {
		gap: 4,
		headerTitle: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.title"),
		headerSubtitle: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.subtitle"),
		children: /* @__PURE__ */ jsxs(Form.Root, {
			gap: 4,
			onSubmit,
			children: [
				/* @__PURE__ */ jsxs(SegmentedControl.Root, {
					"aria-label": t(localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.modeLabel")),
					value: mode,
					onChange: (next) => changeMode(next),
					size: "lg",
					sx: { alignSelf: "flex-start" },
					children: [/* @__PURE__ */ jsx(SegmentedControl.Button, {
						value: "email",
						text: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.mode__email")
					}), /* @__PURE__ */ jsx(SegmentedControl.Button, {
						value: "role",
						text: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.mode__role")
					})]
				}),
				mode === "email" ? /* @__PURE__ */ jsxs(Col, {
					gap: 2,
					children: [/* @__PURE__ */ jsx(Form.ControlRow, {
						elementId: emailField.id,
						children: /* @__PURE__ */ jsx(Form.PlainInput, {
							...emailField.props,
							autoFocus: true,
							ignorePasswordManager: true,
							elementDescriptor: descriptors.organizationProfileSecuritySsoBypassEmailInput
						})
					}), failure && /* @__PURE__ */ jsx(InlineMessage, {
						icon: SvgExclamationTriangle,
						text: failure,
						elementDescriptor: descriptors.organizationProfileSecuritySsoBypassFailure
					})]
				}) : /* @__PURE__ */ jsxs(Col, {
					gap: 2,
					children: [
						/* @__PURE__ */ jsx(RoleSelect, {
							roles,
							value: role,
							formatLabel: formatRoleLabel,
							onChange: setRole,
							isDisabled: card.isLoading,
							triggerSx: (t) => ({
								width: "100%",
								justifyContent: "space-between",
								color: t.colors.$colorForeground
							})
						}),
						failure && /* @__PURE__ */ jsx(InlineMessage, {
							icon: SvgExclamationTriangle,
							text: failure,
							elementDescriptor: descriptors.organizationProfileSecuritySsoBypassFailure
						}),
						/* @__PURE__ */ jsx(InlineMessage, {
							icon: SvgInformationCircle,
							text: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.roleWarning"),
							elementDescriptor: descriptors.organizationProfileSecuritySsoBypassRoleWarning
						})
					]
				}),
				/* @__PURE__ */ jsx(FormButtons, {
					isDisabled: !canSubmit,
					submitLabel: localizationKeys("organizationProfile.securityPage.ssoBypassPage.addForm.submitButton"),
					onReset
				})
			]
		})
	});
});

//#endregion
export { SSOBypassAllowlistPage };
//# sourceMappingURL=SSOBypassAllowlistPage.js.map