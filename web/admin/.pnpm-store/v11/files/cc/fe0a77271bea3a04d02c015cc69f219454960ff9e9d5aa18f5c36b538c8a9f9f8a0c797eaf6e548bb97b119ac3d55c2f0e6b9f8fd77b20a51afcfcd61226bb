import { isEnterpriseConnectionConfigured, organizationEnterpriseConnection } from "../domain/organizationEnterpriseConnection.js";
import { __internal_useOrganizationEnterpriseConnectionTestRuns } from "@clerk/shared/react";

//#region src/components/ConfigureSSO/hooks/useOrganizationEnterpriseConnectionStatus.ts
/** Same probe and query key as the umbrella hook, so react-query dedupes it for the scoped connection. */
const useOrganizationEnterpriseConnectionStatus = (connection, { probe = true } = {}) => {
	const isConfigured = isEnterpriseConnectionConfigured(connection);
	const { data: successfulTestRuns } = __internal_useOrganizationEnterpriseConnectionTestRuns({
		enterpriseConnectionId: connection.id,
		params: {
			initialPage: 1,
			pageSize: 1,
			status: ["success"]
		},
		enabled: probe && isConfigured && !connection.active
	});
	return organizationEnterpriseConnection({
		connection,
		hasSuccessfulTestRun: probe ? (successfulTestRuns?.length ?? 0) > 0 : isConfigured
	});
};

//#endregion
export { useOrganizationEnterpriseConnectionStatus };
//# sourceMappingURL=useOrganizationEnterpriseConnectionStatus.js.map