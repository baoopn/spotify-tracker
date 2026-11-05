import { defaultConnectionDomains, domainsClaimedByOtherConnections, isEnterpriseConnectionConfigured, organizationEnterpriseConnection, sortEnterpriseConnections } from "../domain/organizationEnterpriseConnection.js";
import { useEnterpriseConnectionTestRuns } from "./useEnterpriseConnectionTestRuns.js";
import { useCallback, useMemo, useRef, useState } from "react";
import { __internal_useOrganizationDomains, __internal_useOrganizationEnterpriseConnections, useOrganization, useSession, useUser } from "@clerk/shared/react";

//#region src/components/ConfigureSSO/hooks/useOrganizationEnterpriseConnection.ts
/**
* Umbrella hook for the active organization's enterprise connection. Composes
* the source query, the domain aggregate, the mutations, and the test-run state
* into one surface, exposing a single `isLoading` flag so the caller can gate
* the skeleton above the provider.
*
* `__internal_useOrganizationEnterpriseConnections` is the single swappable
* seam: a future non-org context only swaps this source and everything below
* stays put.
*/
const useOrganizationEnterpriseConnection = ({ manage = true } = {}) => {
	const { data: sourceConnections, isLoading: isLoadingEnterpriseConnections, createEnterpriseConnection, updateEnterpriseConnection, deleteEnterpriseConnection } = __internal_useOrganizationEnterpriseConnections({ enabled: true });
	const enterpriseConnections = useMemo(() => sortEnterpriseConnections(sourceConnections ?? []), [sourceConnections]);
	const [requestedScope, setRequestedScope] = useState(null);
	const [draftDomains, setDraftDomains] = useState(null);
	const setScope = useCallback((next) => {
		setRequestedScope(next);
		setDraftDomains(null);
	}, []);
	const connectionScope = useMemo(() => requestedScope ?? (enterpriseConnections[0] ? {
		kind: "existing",
		id: enterpriseConnections[0].id
	} : { kind: "new" }), [requestedScope, enterpriseConnections]);
	const enterpriseConnection = connectionScope.kind === "existing" ? enterpriseConnections.find((connection) => connection.id === connectionScope.id) : void 0;
	const selectConnection = useCallback((next) => setScope(next), [setScope]);
	const hadInitialConnectionRef = useRef(void 0);
	if (hadInitialConnectionRef.current === void 0 && !isLoadingEnterpriseConnections) hadInitialConnectionRef.current = Boolean(enterpriseConnection);
	const hadInitialConnection = hadInitialConnectionRef.current === true;
	const { hasSuccessfulTestRun, isLoading: isLoadingTestRuns, isFetching: isFetchingTestRuns, rows: testRunRows, totalCount: testRunTotalCount, isPolling: isPollingTestRuns, page: testRunPage, setPage: setTestRunPage, refresh: refreshTestRuns, revalidateHasSuccessfulTestRun } = useEnterpriseConnectionTestRuns(enterpriseConnection, (isEnterpriseConnectionConfigured(enterpriseConnection) || Boolean(enterpriseConnection?.active)) && manage);
	const { user } = useUser();
	const { session } = useSession();
	const { organization } = useOrganization();
	const claimedDomains = useMemo(() => domainsClaimedByOtherConnections(enterpriseConnections, enterpriseConnection?.id), [enterpriseConnections, enterpriseConnection]);
	const { isLoading: isLoadingOrganizationDomains, data: organizationDomains, createDomain, prepareOwnershipVerification, attemptOwnershipVerification, revalidate: revalidateDomains } = __internal_useOrganizationDomains({
		enabled: manage,
		enrollmentMode: "enterprise_sso",
		onOwnershipVerified: useCallback(async (verifiedDomains) => {
			const current = enterpriseConnection ? enterpriseConnection.domains ?? [] : draftDomains;
			if (current === null) return;
			const domains = Array.from(new Set([...current, ...verifiedDomains.map((domain) => domain.name).filter((name) => !claimedDomains.has(name))]));
			if (domains.length === current.length) return;
			if (enterpriseConnection) await updateEnterpriseConnection(enterpriseConnection.id, { domains });
			else setDraftDomains(domains);
		}, [
			enterpriseConnection,
			draftDomains,
			claimedDomains,
			updateEnterpriseConnection
		])
	});
	const connectionDomains = useMemo(() => enterpriseConnection ? enterpriseConnection.domains ?? [] : draftDomains ?? defaultConnectionDomains(organizationDomains, claimedDomains), [
		enterpriseConnection,
		draftDomains,
		organizationDomains,
		claimedDomains
	]);
	const setConnectionDomains = useCallback(async (domains) => {
		if (enterpriseConnection) await updateEnterpriseConnection(enterpriseConnection.id, { domains });
		else setDraftDomains(domains);
	}, [enterpriseConnection, updateEnterpriseConnection]);
	const organizationDomainMutations = useMemo(() => ({
		createDomain,
		prepareOwnershipVerification,
		attemptOwnershipVerification,
		revalidate: revalidateDomains
	}), [
		createDomain,
		prepareOwnershipVerification,
		attemptOwnershipVerification,
		revalidateDomains
	]);
	const enterpriseConnectionMutations = useMemo(() => {
		const createConnection = async (provider) => {
			const created = await createEnterpriseConnection({
				provider,
				domains: connectionDomains
			});
			if (created) setScope({
				kind: "existing",
				id: created.id
			});
			return created;
		};
		const changeProvider = async (id, provider) => {
			const replaced = enterpriseConnections.find((connection) => connection.id === id);
			await deleteEnterpriseConnection(id);
			const created = await createEnterpriseConnection({
				provider,
				domains: replaced?.domains ?? connectionDomains
			});
			if (created) setScope({
				kind: "existing",
				id: created.id
			});
			return created;
		};
		const updateConnection = (id, params) => updateEnterpriseConnection(id, params);
		const setConnectionActive = (id, active) => updateEnterpriseConnection(id, { active });
		const deleteConnection = async (id) => {
			const deleted = await deleteEnterpriseConnection(id);
			if (connectionScope.kind === "existing" && connectionScope.id === id) setScope({ kind: "new" });
			return deleted;
		};
		const createTestRun = (id) => {
			if (!organization) throw new Error("useOrganizationEnterpriseConnection.createTestRun called before the organization resource was loaded.");
			return organization.createEnterpriseConnectionTestRun(id);
		};
		return {
			createConnection,
			changeProvider,
			updateConnection,
			setConnectionActive,
			deleteConnection,
			createTestRun
		};
	}, [
		organization,
		connectionDomains,
		enterpriseConnections,
		connectionScope,
		setScope,
		createEnterpriseConnection,
		updateEnterpriseConnection,
		deleteEnterpriseConnection
	]);
	const testRuns = useMemo(() => ({
		rows: testRunRows,
		totalCount: testRunTotalCount,
		isLoading: isLoadingTestRuns,
		isFetching: isFetchingTestRuns,
		isPolling: isPollingTestRuns,
		page: testRunPage,
		setPage: setTestRunPage,
		refresh: refreshTestRuns,
		revalidateHasSuccessfulTestRun
	}), [
		testRunRows,
		testRunTotalCount,
		isLoadingTestRuns,
		isFetchingTestRuns,
		isPollingTestRuns,
		testRunPage,
		setTestRunPage,
		refreshTestRuns,
		revalidateHasSuccessfulTestRun
	]);
	const organizationEnterpriseConnection$1 = useMemo(() => organizationEnterpriseConnection({
		connection: enterpriseConnection,
		hasSuccessfulTestRun
	}), [enterpriseConnection, hasSuccessfulTestRun]);
	return {
		user,
		session,
		organization,
		isLoading: isLoadingEnterpriseConnections || isLoadingOrganizationDomains || hadInitialConnection && isLoadingTestRuns,
		enterpriseConnections,
		connectionScope,
		selectConnection,
		enterpriseConnection,
		connectionDomains,
		setConnectionDomains,
		claimedDomains,
		organizationEnterpriseConnection: organizationEnterpriseConnection$1,
		enterpriseConnectionMutations,
		testRuns,
		organizationDomains,
		organizationDomainMutations
	};
};

//#endregion
export { useOrganizationEnterpriseConnection };
//# sourceMappingURL=useOrganizationEnterpriseConnection.js.map