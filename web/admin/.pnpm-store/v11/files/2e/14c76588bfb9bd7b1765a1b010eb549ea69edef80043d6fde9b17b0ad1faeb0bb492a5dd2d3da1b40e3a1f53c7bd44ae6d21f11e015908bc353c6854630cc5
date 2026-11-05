import { CardStateProvider } from "../../elements/contexts/index.js";
import { Wizard } from "./elements/Wizard/Wizard.js";
import { ConfigureSSOHeader } from "./ConfigureSSOHeader.js";
import { ConfigureSSOProvider } from "./ConfigureSSOContext.js";
import { areConnectionDomainsReady } from "./domain/organizationEnterpriseConnection.js";
import { ActivateStep } from "./steps/ActivateStep.js";
import { ConfigureStep } from "./steps/ConfigureStep/index.js";
import { OrganizationDomainsStep } from "./steps/OrganizationDomainsStep.js";
import { TestConfigurationStep } from "./steps/TestConfigurationStep.js";
import React from "react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/ConfigureSSO/ConfigureSSOWizard.tsx
const ConfigureSSOWizard = ({ title, forceInitialStep, ...props }) => {
	const { organizationEnterpriseConnection: c, connectionDomains, claimedDomains, organizationDomains } = props;
	const domainsReady = areConnectionDomainsReady(connectionDomains, organizationDomains, claimedDomains);
	const steps = React.useMemo(() => [
		{
			id: "verify-domain",
			label: "Domains",
			isComplete: () => domainsReady
		},
		{
			id: "configure",
			label: "Connection",
			isReachable: () => domainsReady || c.hasConnection,
			isComplete: () => c.hasMinimumConfiguration || c.isActive
		},
		{
			id: "test",
			label: "Test",
			isReachable: () => c.hasMinimumConfiguration || c.isActive,
			isComplete: () => c.hasSuccessfulTestRun || c.isActive
		},
		{
			id: "activate",
			label: "Activate",
			isReachable: () => c.hasSuccessfulTestRun || c.isActive,
			isComplete: () => c.isActive
		}
	], [c, domainsReady]);
	const initialStepId = forceInitialStep ? steps[0].id : void 0;
	return /* @__PURE__ */ jsx(ConfigureSSOProvider, {
		...props,
		children: /* @__PURE__ */ jsxs(Wizard, {
			steps,
			initialStepId,
			children: [
				/* @__PURE__ */ jsx(ConfigureSSOHeader, { title }),
				/* @__PURE__ */ jsx(Wizard.Match, {
					id: "verify-domain",
					children: /* @__PURE__ */ jsx(CardStateProvider, { children: /* @__PURE__ */ jsx(OrganizationDomainsStep, {}) })
				}),
				/* @__PURE__ */ jsx(Wizard.Match, {
					id: "configure",
					children: /* @__PURE__ */ jsx(CardStateProvider, { children: /* @__PURE__ */ jsx(ConfigureStep, {}) })
				}),
				/* @__PURE__ */ jsx(Wizard.Match, {
					id: "test",
					children: /* @__PURE__ */ jsx(CardStateProvider, { children: /* @__PURE__ */ jsx(TestConfigurationStep, {}) })
				}),
				/* @__PURE__ */ jsx(Wizard.Match, {
					id: "activate",
					children: /* @__PURE__ */ jsx(CardStateProvider, { children: /* @__PURE__ */ jsx(ActivateStep, {}) })
				})
			]
		})
	});
};

//#endregion
export { ConfigureSSOWizard };
//# sourceMappingURL=ConfigureSSOWizard.js.map