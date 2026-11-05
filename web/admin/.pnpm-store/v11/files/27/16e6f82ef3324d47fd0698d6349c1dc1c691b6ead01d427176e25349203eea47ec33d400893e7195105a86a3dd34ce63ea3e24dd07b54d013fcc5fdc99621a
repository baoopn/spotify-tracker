import { localizationKeys } from "../localization/localizationKeys.js";
import SvgExclamationTriangle from "../icons/exclamation-triangle.js";
import { descriptors } from "../customizables/elementDescriptors.js";
import { Flow } from "../customizables/Flow.js";
import { Col, Flex, Icon, Text } from "../customizables/index.js";
import { Card } from "../elements/Card/index.js";
import { Header } from "../elements/Header.js";
import { safeHref } from "../utils/actionBlocked.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/common/ActionBlockedCard.tsx
const ActionBlockedCard = (props) => {
	const { traceId, title, description, linkUrl, linkText } = props.details;
	const href = safeHref(linkUrl);
	return /* @__PURE__ */ jsx(Flow.Part, {
		part: "actionBlocked",
		children: /* @__PURE__ */ jsxs(Card.Root, { children: [/* @__PURE__ */ jsxs(Card.Content, { children: [/* @__PURE__ */ jsxs(Header.Root, { children: [title ? /* @__PURE__ */ jsx(Header.Title, { children: title }) : /* @__PURE__ */ jsx(Header.Title, { localizationKey: localizationKeys("actionBlocked.title") }), description ? /* @__PURE__ */ jsx(Header.Subtitle, { children: description }) : /* @__PURE__ */ jsx(Header.Subtitle, { localizationKey: localizationKeys("actionBlocked.subtitle") })] }), /* @__PURE__ */ jsxs(Col, {
			elementDescriptor: descriptors.main,
			gap: 6,
			children: [
				/* @__PURE__ */ jsx(Flex, {
					elementDescriptor: descriptors.actionBlockedIconBox,
					center: true,
					sx: (theme) => ({
						alignSelf: "center",
						width: theme.sizes.$16,
						height: theme.sizes.$16,
						borderRadius: theme.radii.$circle,
						backgroundColor: theme.colors.$neutralAlpha100,
						color: theme.colors.$danger500
					}),
					children: /* @__PURE__ */ jsx(Icon, {
						elementDescriptor: descriptors.actionBlockedIcon,
						icon: SvgExclamationTriangle,
						sx: (theme) => ({
							height: theme.sizes.$5,
							width: theme.sizes.$5
						})
					})
				}),
				href ? /* @__PURE__ */ jsx(Text, {
					elementDescriptor: descriptors.actionBlockedLink,
					as: "a",
					variant: "buttonLarge",
					colorScheme: "inherit",
					sx: {
						textAlign: "center",
						textDecoration: "underline"
					},
					href,
					target: "_blank",
					rel: "noopener noreferrer",
					children: linkText || href
				}) : null,
				traceId ? /* @__PURE__ */ jsxs(Col, {
					elementDescriptor: descriptors.actionBlockedTraceIdBox,
					gap: 1,
					sx: { alignItems: "center" },
					children: [/* @__PURE__ */ jsx(Text, {
						elementDescriptor: descriptors.actionBlockedTraceIdLabel,
						variant: "caption",
						colorScheme: "secondary",
						localizationKey: localizationKeys("actionBlocked.traceIdLabel")
					}), /* @__PURE__ */ jsx(Text, {
						elementDescriptor: descriptors.actionBlockedTraceId,
						variant: "body",
						colorScheme: "secondary",
						sx: (theme) => ({
							fontFamily: theme.fonts.$buttons,
							userSelect: "all",
							letterSpacing: theme.space.$xxs
						}),
						children: traceId
					})]
				}) : null
			]
		})] }), /* @__PURE__ */ jsx(Card.Footer, {})] })
	});
};

//#endregion
export { ActionBlockedCard };
//# sourceMappingURL=ActionBlockedCard.js.map