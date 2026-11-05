import { localizationKeys } from "../../../localization/localizationKeys.js";
import { descriptors } from "../../../customizables/elementDescriptors.js";
import { Table, Tbody, Td, Text, Th, Thead, Tr } from "../../../customizables/index.js";
import { useWizard } from "../../ConfigureSSO/elements/Wizard/WizardContext.js";
import { Step } from "../../ConfigureSSO/elements/Step.js";
import { useConfigureDirectorySync } from "../ConfigureDirectorySyncContext.js";
import { Fragment, jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/ConfigureDirectorySync/steps/AttributeMappingStep.tsx
const AttributeValue = ({ column, children }) => /* @__PURE__ */ jsx(Text, {
	elementDescriptor: descriptors.configureDirectorySyncAttributeMappingValue,
	elementId: descriptors.configureDirectorySyncAttributeMappingValue.setId(column),
	as: "code",
	sx: (t) => ({
		fontFamily: "monospace",
		fontSize: t.fontSizes.$sm
	}),
	children
});
const ColumnHeader = ({ column, localizationKey }) => /* @__PURE__ */ jsx(Text, {
	elementDescriptor: descriptors.configureDirectorySyncAttributeMappingHeader,
	elementId: descriptors.configureDirectorySyncAttributeMappingHeader.setId(column),
	as: "span",
	colorScheme: "secondary",
	localizationKey,
	sx: (t) => ({
		fontSize: t.fontSizes.$sm,
		fontWeight: t.fontWeights.$normal
	})
});
const AttributeMappingStep = () => {
	const { goNext, goPrev } = useWizard();
	const { directory } = useConfigureDirectorySync();
	const rows = Object.entries(directory?.attributeMapping ?? {}).map(([clerkAttribute, scimPath]) => ({
		clerkAttribute,
		scimPath
	})).sort((a, b) => a.scimPath.localeCompare(b.scimPath));
	return /* @__PURE__ */ jsxs(Fragment, { children: [
		/* @__PURE__ */ jsx(Step.Header, {
			title: localizationKeys("configureDirectorySync.attributeMappingStep.title"),
			description: localizationKeys("configureDirectorySync.attributeMappingStep.subtitle")
		}),
		/* @__PURE__ */ jsx(Step.Body, { children: /* @__PURE__ */ jsx(Step.Section, {
			sx: (t) => ({ gap: t.space.$5 }),
			children: /* @__PURE__ */ jsxs(Table, {
				elementDescriptor: descriptors.configureDirectorySyncAttributeMappingTable,
				sx: (t) => ({
					"tr > th": {
						paddingBlock: t.space.$2,
						paddingInline: t.space.$4
					},
					"tr > td": { paddingBlock: t.space.$3 }
				}),
				children: [/* @__PURE__ */ jsx(Thead, { children: /* @__PURE__ */ jsxs(Tr, { children: [/* @__PURE__ */ jsx(Th, { children: /* @__PURE__ */ jsx(ColumnHeader, {
					column: "directory",
					localizationKey: localizationKeys("configureDirectorySync.attributeMappingStep.columns.directoryAttribute")
				}) }), /* @__PURE__ */ jsx(Th, { children: /* @__PURE__ */ jsx(ColumnHeader, {
					column: "clerk",
					localizationKey: localizationKeys("configureDirectorySync.attributeMappingStep.columns.clerkAttribute")
				}) })] }) }), /* @__PURE__ */ jsx(Tbody, { children: rows.map(({ clerkAttribute, scimPath }) => /* @__PURE__ */ jsxs(Tr, { children: [/* @__PURE__ */ jsx(Td, { children: /* @__PURE__ */ jsx(AttributeValue, {
					column: "directory",
					children: scimPath
				}) }), /* @__PURE__ */ jsx(Td, { children: /* @__PURE__ */ jsx(AttributeValue, {
					column: "clerk",
					children: clerkAttribute
				}) })] }, clerkAttribute)) })]
			})
		}) }),
		/* @__PURE__ */ jsxs(Step.Footer, { children: [/* @__PURE__ */ jsx(Step.Footer.Previous, { onClick: () => goPrev() }), /* @__PURE__ */ jsx(Step.Footer.Continue, { onClick: () => goNext() })] })
	] });
};

//#endregion
export { AttributeMappingStep };
//# sourceMappingURL=AttributeMappingStep.js.map