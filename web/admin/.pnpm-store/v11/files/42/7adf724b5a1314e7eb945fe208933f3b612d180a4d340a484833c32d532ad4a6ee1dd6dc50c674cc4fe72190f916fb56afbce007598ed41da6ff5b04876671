import React from "react";
import { jsx } from "@emotion/react/jsx-runtime";

//#region src/components/ConfigureSSO/ConfigureSSOContext.tsx
const ConfigureSSOContext = React.createContext(null);
ConfigureSSOContext.displayName = "ConfigureSSOContext";
const ConfigureSSOProvider = ({ enterpriseConnection, connectionDomains, setConnectionDomains, claimedDomains, organizationEnterpriseConnection, testRuns, organizationDomains, contentRef, enterpriseConnectionMutations, organizationDomainMutations, onExit, children }) => {
	const value = React.useMemo(() => ({
		contentRef,
		enterpriseConnection,
		connectionDomains,
		setConnectionDomains,
		claimedDomains,
		organizationEnterpriseConnection,
		testRuns,
		organizationDomains,
		enterpriseConnectionMutations,
		organizationDomainMutations,
		onExit
	}), [
		contentRef,
		enterpriseConnectionMutations,
		organizationDomainMutations,
		organizationEnterpriseConnection,
		testRuns,
		organizationDomains,
		enterpriseConnection,
		connectionDomains,
		setConnectionDomains,
		claimedDomains,
		onExit
	]);
	return /* @__PURE__ */ jsx(ConfigureSSOContext.Provider, {
		value,
		children
	});
};
const useConfigureSSO = () => {
	const ctx = React.useContext(ConfigureSSOContext);
	if (!ctx) throw new Error("useConfigureSSO called outside <ConfigureSSOProvider>.");
	return ctx;
};

//#endregion
export { ConfigureSSOProvider, useConfigureSSO };
//# sourceMappingURL=ConfigureSSOContext.js.map