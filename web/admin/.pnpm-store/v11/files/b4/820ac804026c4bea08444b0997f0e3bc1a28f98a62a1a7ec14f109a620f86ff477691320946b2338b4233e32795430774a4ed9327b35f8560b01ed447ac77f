//#region src/components/ConfigureSSO/domain/organizationEnterpriseConnection.ts
const isOidcProvider = (provider) => provider.startsWith("oidc_") || provider.startsWith("oauth_custom_");
/** FAPI returns the list unordered; every reader sorts through here so they agree on "the first one". */
const sortEnterpriseConnections = (connections) => [...connections].sort((a, b) => {
	const aCreatedAt = a.createdAt?.getTime();
	const bCreatedAt = b.createdAt?.getTime();
	if (aCreatedAt !== bCreatedAt) {
		if (aCreatedAt === void 0) return 1;
		if (bCreatedAt === void 0) return -1;
		return aCreatedAt - bCreatedAt;
	}
	return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
});
const isEnterpriseConnectionConfigured = (connection) => {
	if (!connection) return false;
	if (isOidcProvider(connection.provider)) {
		const oauthConfig = connection.oauthConfig;
		return Boolean(oauthConfig?.clientId && (oauthConfig.discoveryUrl || oauthConfig.authUrl && oauthConfig.tokenUrl));
	}
	return Boolean(connection.samlConnection?.idpSsoUrl && connection.samlConnection?.idpEntityId);
};
const isOrganizationDomainVerified = (domain) => domain.ownershipVerification?.status === "verified";
/**
* Domains every connection other than `scopedConnectionId` already authenticates,
* keyed to that connection's name. FAPI rejects a domain shared by two
* connections of the same instance, so the wizard never offers these.
*/
const domainsClaimedByOtherConnections = (connections, scopedConnectionId) => {
	const claimed = /* @__PURE__ */ new Map();
	for (const connection of connections) {
		if (connection.id === scopedConnectionId) continue;
		for (const domain of connection.domains ?? []) claimed.set(domain, connection.name);
	}
	return claimed;
};
/**
* The domains a connection would receive before its admin touches the
* selection: every verified organization domain no other connection claims.
*/
const defaultConnectionDomains = (organizationDomains, claimed) => (organizationDomains ?? []).filter((domain) => isOrganizationDomainVerified(domain) && !claimed.has(domain.name)).map((domain) => domain.name);
/**
* Whether the connection's domains let the wizard move past the domains step:
* at least one domain, none of them still pending verification. A connection
* domain missing from the organization list was accepted by FAPI already, so it
* does not block.
*/
const areConnectionDomainsReady = (connectionDomains, organizationDomains, claimedDomains) => connectionDomains.length > 0 && connectionDomains.every((name) => {
	if (claimedDomains?.has(name)) return false;
	const organizationDomain = organizationDomains?.find((domain) => domain.name === name);
	return !organizationDomain || isOrganizationDomainVerified(organizationDomain);
});
const connectionStatus = ({ hasConnection, isActive, hasMinimumConfiguration, hasSuccessfulTestRun }) => {
	if (!hasConnection) return "unconfigured";
	if (isActive) return "active";
	if (hasMinimumConfiguration && hasSuccessfulTestRun) return "inactive";
	return "in_progress";
};
const organizationEnterpriseConnection = ({ connection, hasSuccessfulTestRun }) => {
	const hasConnection = Boolean(connection);
	const isActive = Boolean(connection?.active);
	const hasMinimumConfiguration = isEnterpriseConnectionConfigured(connection);
	return {
		provider: connection?.provider,
		hasConnection,
		isActive,
		hasMinimumConfiguration,
		hasSuccessfulTestRun,
		status: connectionStatus({
			hasConnection,
			isActive,
			hasMinimumConfiguration,
			hasSuccessfulTestRun
		})
	};
};

//#endregion
export { areConnectionDomainsReady, defaultConnectionDomains, domainsClaimedByOtherConnections, isEnterpriseConnectionConfigured, isOidcProvider, isOrganizationDomainVerified, organizationEnterpriseConnection, sortEnterpriseConnections };
//# sourceMappingURL=organizationEnterpriseConnection.js.map