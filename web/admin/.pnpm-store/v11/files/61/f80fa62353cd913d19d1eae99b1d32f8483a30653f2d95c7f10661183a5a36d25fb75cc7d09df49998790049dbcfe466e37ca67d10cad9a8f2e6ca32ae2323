import { withCoreUserGuard } from "../../contexts/CoreUserContext.js";
import { useEnvironment } from "../../contexts/EnvironmentContext.js";
import { localizationKeys } from "../../localization/localizationKeys.js";
import { Route } from "../../router/Route.js";
import { Switch } from "../../router/Switch.js";
import { useLocalizations } from "../../localization/makeLocalizable.js";
import { Alert } from "../../primitives/Alert.js";
import { useFormControl } from "../../utils/useFormControl.js";
import { withCardStateProvider } from "../../elements/contexts/index.js";
import { Flow } from "../../customizables/Flow.js";
import { Button, Col, Grid, Image, Text } from "../../customizables/index.js";
import { ApplicationLogo } from "../../elements/ApplicationLogo.js";
import { Card } from "../../elements/Card/index.js";
import { Header } from "../../elements/Header.js";
import { Form } from "../../elements/Form.js";
import { LoadingCardContainer } from "../../elements/LoadingCard.js";
import { ListGroup, ListGroupContent, ListGroupHeader, ListGroupHeaderTitle, ListGroupItem, ListGroupItemLabel } from "../OAuthConsent/ListGroup.js";
import { LogoGroup, LogoGroupIcon, LogoGroupItem, LogoGroupItemContainer, LogoGroupSeparator } from "../OAuthConsent/LogoGroup.js";
import { OrgSelect } from "../OAuthConsent/OrgSelect.js";
import { getOAuthDeviceUserCodeFromSearch, isValidOAuthDeviceUserCode, normalizeOAuthDeviceUserCode } from "./utils.js";
import { OAuthDeviceVerificationCodeInput } from "./OAuthDeviceVerificationCodeInput.js";
import { isClerkAPIResponseError } from "@clerk/shared/error";
import { useCallback, useEffect, useRef, useState } from "react";
import { useClerk, useOAuthDeviceVerification, useUser } from "@clerk/shared/react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/OAuthDeviceVerification/OAuthDeviceVerification.tsx
const USER_ORG_READ_SCOPE = "user:org:read";
const OFFLINE_ACCESS_SCOPE = "offline_access";
const PRIVATE_METADATA_SCOPE = "private_metadata";
function getErrorCode(error) {
	return isClerkAPIResponseError(error) ? error.errors?.[0]?.code : void 0;
}
function OAuthDeviceVerificationInternal() {
	const clerk = useClerk();
	const { user } = useUser();
	const { displayConfig: { applicationName, logoImageUrl }, organizationSettings } = useEnvironment();
	const { t } = useLocalizations();
	const verification = useOAuthDeviceVerification();
	const [view, setView] = useState("entry");
	const [selectedOrg, setSelectedOrg] = useState(null);
	const [submittingDecision, setSubmittingDecision] = useState(null);
	const handledPrefill = useRef(false);
	const decisionInProgress = useRef(false);
	const codeControl = useFormControl("userCode", "", {
		type: "text",
		label: localizationKeys("oauthDeviceVerification.start.userCodeLabel"),
		transformer: normalizeOAuthDeviceUserCode,
		isRequired: true
	});
	const showLookupError = useCallback((error) => {
		switch (getErrorCode(error)) {
			case "resource_not_found":
				setView("entry");
				codeControl.setError(t(localizationKeys("oauthDeviceVerification.error.unknownCode")));
				break;
			case "oauth_device_code_expired":
				setView("expired");
				break;
			case "too_many_requests":
				setView("rateLimited");
				break;
			default: setView("error");
		}
	}, [codeControl, t]);
	const showLookupResult = useCallback((info) => {
		switch (info.status) {
			case "pending":
				setView("confirmation");
				break;
			case "approved":
				setView("alreadyApproved");
				break;
			case "denied":
				setView("alreadyDenied");
				break;
			case "consumed":
				setView("consumed");
				break;
			default: setView("error");
		}
	}, []);
	const lookup = useCallback(async (userCode) => {
		codeControl.clearFeedback();
		setView("loading");
		try {
			showLookupResult(await verification.lookup({ userCode }));
		} catch (error) {
			showLookupError(error);
		}
	}, [
		codeControl,
		showLookupError,
		showLookupResult,
		verification
	]);
	useEffect(() => {
		if (handledPrefill.current) return;
		handledPrefill.current = true;
		const userCode = getOAuthDeviceUserCodeFromSearch();
		if (!userCode) return;
		codeControl.setValue(normalizeOAuthDeviceUserCode(userCode));
		if (!isValidOAuthDeviceUserCode(userCode)) {
			codeControl.setError(t(localizationKeys("oauthDeviceVerification.error.invalidCode")));
			return;
		}
		lookup(normalizeOAuthDeviceUserCode(userCode));
	}, [
		codeControl,
		lookup,
		t
	]);
	const handleLookup = (event) => {
		event.preventDefault();
		const userCode = normalizeOAuthDeviceUserCode(codeControl.value);
		if (!isValidOAuthDeviceUserCode(userCode)) {
			codeControl.setError(t(localizationKeys("oauthDeviceVerification.error.invalidCode")));
			return;
		}
		lookup(userCode);
	};
	const data = verification.data;
	const orgSelectionEnabled = (data?.scopes.some((scope) => scope.scope === USER_ORG_READ_SCOPE) ?? false) && organizationSettings.enabled;
	const orgOptions = orgSelectionEnabled ? (user?.organizationMemberships ?? []).map((membership) => ({
		value: membership.organization.id,
		label: membership.organization.name,
		logoUrl: membership.organization.imageUrl
	})) : [];
	const lastActiveOrgId = clerk.session?.lastActiveOrganizationId;
	const defaultOrg = orgOptions.find((option) => option.value === lastActiveOrgId)?.value ?? orgOptions[0]?.value ?? null;
	const effectiveOrg = selectedOrg ?? defaultOrg;
	const showDecisionError = (error) => {
		switch (getErrorCode(error)) {
			case "oauth_device_code_expired":
				setView("expired");
				break;
			case "too_many_requests":
				setView("rateLimited");
				break;
			case "bad_request":
				setView("alreadyDecided");
				break;
			default: setView("error");
		}
	};
	const handleApprove = async () => {
		if (decisionInProgress.current) return;
		decisionInProgress.current = true;
		const userCode = normalizeOAuthDeviceUserCode(codeControl.value);
		setSubmittingDecision("approve");
		try {
			await verification.approve({
				userCode,
				organizationId: effectiveOrg ?? void 0
			});
			setView("approved");
		} catch (error) {
			showDecisionError(error);
		} finally {
			decisionInProgress.current = false;
			setSubmittingDecision(null);
		}
	};
	const handleDeny = async () => {
		if (decisionInProgress.current) return;
		decisionInProgress.current = true;
		const userCode = normalizeOAuthDeviceUserCode(codeControl.value);
		setSubmittingDecision("deny");
		try {
			await verification.deny({ userCode });
			setView("denied");
		} catch (error) {
			showDecisionError(error);
		} finally {
			decisionInProgress.current = false;
			setSubmittingDecision(null);
		}
	};
	const reset = () => {
		decisionInProgress.current = false;
		verification.reset();
		codeControl.setValue("");
		codeControl.clearFeedback();
		setSelectedOrg(null);
		setView("entry");
	};
	if (view === "loading") return /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsx(Card.Content, { children: /* @__PURE__ */ jsx(LoadingCardContainer, {}) }), /* @__PURE__ */ jsx(Card.Footer, {})] });
	if (view === "entry") return /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsxs(Card.Content, { children: [/* @__PURE__ */ jsxs(Header.Root, {
		showLogo: true,
		children: [/* @__PURE__ */ jsx(Header.Title, { localizationKey: localizationKeys("oauthDeviceVerification.start.title") }), /* @__PURE__ */ jsx(Header.Subtitle, { localizationKey: localizationKeys("oauthDeviceVerification.start.subtitle") })]
	}), /* @__PURE__ */ jsxs(Form.Root, {
		onSubmit: handleLookup,
		children: [/* @__PURE__ */ jsx(Form.ControlRow, {
			elementId: codeControl.id,
			children: /* @__PURE__ */ jsx(Form.CommonInputWrapper, {
				...codeControl.props,
				children: /* @__PURE__ */ jsx(OAuthDeviceVerificationCodeInput, { control: codeControl })
			})
		}), /* @__PURE__ */ jsx(Form.SubmitButton, { localizationKey: localizationKeys("oauthDeviceVerification.start.action__continue") })]
	})] }), /* @__PURE__ */ jsx(Card.Footer, {})] });
	if (view === "confirmation" && data) {
		const primaryIdentifier = user?.primaryEmailAddress?.emailAddress || user?.primaryPhoneNumber?.phoneNumber || "";
		const displayedScopes = data.scopes.filter((scope) => scope.scope !== OFFLINE_ACCESS_SCOPE).map((scope) => ({
			...scope,
			description: scope.scope === PRIVATE_METADATA_SCOPE ? t(localizationKeys("oauthConsent.scopeList.privateMetadata", { applicationName })) : scope.description
		}));
		const hasOfflineAccess = data.scopes.some((scope) => scope.scope === OFFLINE_ACCESS_SCOPE);
		return /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsxs(Card.Content, { children: [
			/* @__PURE__ */ jsxs(Header.Root, { children: [
				/* @__PURE__ */ jsx(DeviceLogo, {
					applicationName: data.oauthApplicationName,
					logoUrl: data.oauthApplicationLogoUrl,
					showClerkLogo: Boolean(logoImageUrl)
				}),
				/* @__PURE__ */ jsx(Header.Title, { localizationKey: localizationKeys("oauthDeviceVerification.confirmation.title", { applicationName: data.oauthApplicationName }) }),
				/* @__PURE__ */ jsx(Header.Subtitle, { localizationKey: localizationKeys("oauthDeviceVerification.confirmation.subtitle", { identifier: primaryIdentifier }) })
			] }),
			orgSelectionEnabled && orgOptions.length > 0 && effectiveOrg && /* @__PURE__ */ jsx(OrgSelect, {
				options: orgOptions,
				value: effectiveOrg,
				onChange: setSelectedOrg
			}),
			displayedScopes.length > 0 && /* @__PURE__ */ jsxs(ListGroup, { children: [/* @__PURE__ */ jsx(ListGroupHeader, { children: /* @__PURE__ */ jsx(ListGroupHeaderTitle, { localizationKey: localizationKeys("oauthDeviceVerification.confirmation.scopeListTitle", { applicationName: data.oauthApplicationName }) }) }), /* @__PURE__ */ jsx(ListGroupContent, { children: displayedScopes.map((scope) => /* @__PURE__ */ jsx(ListGroupItem, { children: /* @__PURE__ */ jsx(ListGroupItemLabel, { children: scope.description || scope.scope }) }, scope.scope)) })] }),
			/* @__PURE__ */ jsx(Alert, {
				colorScheme: "warning",
				children: /* @__PURE__ */ jsx(Text, {
					colorScheme: "warning",
					variant: "caption",
					localizationKey: localizationKeys("oauthDeviceVerification.confirmation.warning")
				})
			}),
			/* @__PURE__ */ jsxs(Grid, {
				columns: 2,
				gap: 3,
				children: [
					/* @__PURE__ */ jsx(Button, {
						colorScheme: "secondary",
						variant: "outline",
						isLoading: submittingDecision === "deny" && verification.isSubmitting,
						isDisabled: verification.isSubmitting,
						onClick: () => void handleDeny(),
						localizationKey: localizationKeys("oauthDeviceVerification.confirmation.action__deny")
					}),
					/* @__PURE__ */ jsx(Button, {
						isLoading: submittingDecision === "approve" && verification.isSubmitting,
						isDisabled: verification.isSubmitting,
						onClick: () => void handleApprove(),
						localizationKey: localizationKeys("oauthDeviceVerification.confirmation.action__approve")
					}),
					hasOfflineAccess && /* @__PURE__ */ jsx(Text, {
						sx: { gridColumn: "span 2" },
						colorScheme: "secondary",
						variant: "caption",
						localizationKey: localizationKeys("oauthConsent.offlineAccessNotice")
					})
				]
			})
		] }), /* @__PURE__ */ jsx(Card.Footer, {})] });
	}
	if (view === "confirmation") return /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsx(Card.Content, { children: /* @__PURE__ */ jsx(LoadingCardContainer, {}) }), /* @__PURE__ */ jsx(Card.Footer, {})] });
	const terminal = getTerminalContent(view);
	return /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsx(Card.Content, { children: /* @__PURE__ */ jsxs(Col, {
		gap: 6,
		children: [/* @__PURE__ */ jsxs(Header.Root, {
			showLogo: true,
			children: [/* @__PURE__ */ jsx(Header.Title, { localizationKey: localizationKeys(terminal.title) }), /* @__PURE__ */ jsx(Header.Subtitle, { localizationKey: localizationKeys(terminal.subtitle) })]
		}), terminal.canReset && /* @__PURE__ */ jsx(Button, {
			block: true,
			onClick: reset,
			localizationKey: localizationKeys("oauthDeviceVerification.action__tryAnotherCode")
		})]
	}) }), /* @__PURE__ */ jsx(Card.Footer, {})] });
}
function DeviceLogo({ applicationName, logoUrl, showClerkLogo }) {
	const applicationMark = logoUrl ? /* @__PURE__ */ jsx(Image, {
		src: logoUrl,
		alt: applicationName,
		sx: {
			width: "100%",
			height: "100%",
			objectFit: "contain"
		}
	}) : /* @__PURE__ */ jsx(LogoGroupIcon, { label: applicationName });
	if (!showClerkLogo) return /* @__PURE__ */ jsx(LogoGroup, { children: /* @__PURE__ */ jsx(LogoGroupItemContainer, { children: applicationMark }) });
	return /* @__PURE__ */ jsxs(LogoGroup, { children: [
		/* @__PURE__ */ jsx(LogoGroupItem, {
			justify: "end",
			children: /* @__PURE__ */ jsx(LogoGroupItemContainer, { children: applicationMark })
		}),
		/* @__PURE__ */ jsx(LogoGroupSeparator, {}),
		/* @__PURE__ */ jsx(LogoGroupItem, {
			justify: "start",
			children: /* @__PURE__ */ jsx(LogoGroupItemContainer, { children: /* @__PURE__ */ jsx(ApplicationLogo, {}) })
		})
	] });
}
function getTerminalContent(view) {
	switch (view) {
		case "approved": return {
			title: "oauthDeviceVerification.status.approvedTitle",
			subtitle: "oauthDeviceVerification.status.approvedSubtitle",
			canReset: false
		};
		case "alreadyApproved": return {
			title: "oauthDeviceVerification.status.alreadyApprovedTitle",
			subtitle: "oauthDeviceVerification.status.alreadyApprovedSubtitle",
			canReset: false
		};
		case "denied": return {
			title: "oauthDeviceVerification.status.deniedTitle",
			subtitle: "oauthDeviceVerification.status.deniedSubtitle",
			canReset: false
		};
		case "alreadyDenied": return {
			title: "oauthDeviceVerification.status.alreadyDeniedTitle",
			subtitle: "oauthDeviceVerification.status.alreadyDeniedSubtitle",
			canReset: false
		};
		case "consumed": return {
			title: "oauthDeviceVerification.status.consumedTitle",
			subtitle: "oauthDeviceVerification.status.consumedSubtitle",
			canReset: false
		};
		case "expired": return {
			title: "oauthDeviceVerification.error.expiredTitle",
			subtitle: "oauthDeviceVerification.error.expiredSubtitle",
			canReset: false
		};
		case "rateLimited": return {
			title: "oauthDeviceVerification.error.rateLimitedTitle",
			subtitle: "oauthDeviceVerification.error.rateLimitedSubtitle",
			canReset: false
		};
		case "alreadyDecided": return {
			title: "oauthDeviceVerification.status.alreadyDecidedTitle",
			subtitle: "oauthDeviceVerification.status.alreadyDecidedSubtitle",
			canReset: false
		};
		case "error": return {
			title: "oauthDeviceVerification.error.genericTitle",
			subtitle: "oauthDeviceVerification.error.genericSubtitle",
			canReset: true
		};
	}
}
const AuthenticatedRoutes = withCoreUserGuard(withCardStateProvider(OAuthDeviceVerificationInternal));
const OAuthDeviceVerification = () => /* @__PURE__ */ jsx(Flow.Root, {
	flow: "oauthDeviceVerification",
	children: /* @__PURE__ */ jsx(Flow.Part, { children: /* @__PURE__ */ jsx(Switch, { children: /* @__PURE__ */ jsx(Route, { children: /* @__PURE__ */ jsx(AuthenticatedRoutes, {}) }) }) })
});

//#endregion
export { OAuthDeviceVerification };
//# sourceMappingURL=OAuthDeviceVerification.js.map