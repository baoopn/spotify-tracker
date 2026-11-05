//#region src/components/ConfigureSSO/domain/idpCertificates.ts
const EXPIRY_WARNING_DAYS = 30;
const MAX_IDP_CERTIFICATES = 5;
const PEM_HEADER = "-----BEGIN CERTIFICATE-----";
const PEM_FOOTER = "-----END CERTIFICATE-----";
const BASE64_BODY = /^[A-Za-z0-9+/]+=*$/;
/**
* The certificates a connection trusts, primary first. A connection read
* without `idpCertificates` only carries the single certificate and its
* validity columns, which become one entry.
*/
function toIdpCertificateEntries(saml) {
	if (saml?.idpCertificates?.length) return saml.idpCertificates;
	if (saml?.idpCertificate) return [{
		certificate: saml.idpCertificate,
		issuedAt: saml.idpCertificateIssuedAt || null,
		expiresAt: saml.idpCertificateExpiresAt || null
	}];
	return [];
}
/**
* Splits an uploaded file into bare base64 certificate bodies: one per PEM
* block, or the whole file when it holds a single bare base64 certificate.
*/
function parseCertificateFile(text) {
	const chunks = text.split(PEM_HEADER);
	const bodies = [];
	chunks.forEach((chunk, index) => {
		if (index === 0 && chunks.length > 1) return;
		const end = chunk.indexOf(PEM_FOOTER);
		const body = (end >= 0 ? chunk.slice(0, end) : chunk).replace(/\s+/g, "");
		if (body) bodies.push(body);
	});
	return bodies;
}
function areCertificateBodies(bodies) {
	return bodies.length > 0 && bodies.every((body) => BASE64_BODY.test(body));
}
function addCertificates(entries, bodies) {
	const known = new Set(entries.map((entry) => entry.certificate));
	const added = [];
	for (const certificate of bodies) {
		if (entries.length + added.length >= 5) break;
		if (!known.has(certificate)) {
			known.add(certificate);
			added.push({
				certificate,
				issuedAt: null,
				expiresAt: null
			});
		}
	}
	return added.length > 0 ? [...entries, ...added] : entries;
}
function removeCertificate(entries, certificate) {
	return entries.filter((entry) => entry.certificate !== certificate);
}
function haveCertificatesChanged(entries, original) {
	return entries.length !== original.length || entries.some((entry, index) => entry.certificate !== original[index].certificate);
}
function toIdpCertificatesParam(entries) {
	return entries.map((entry) => entry.certificate);
}
function getIdpCertificateStatus(entry, now = Date.now()) {
	if (entry.expiresAt === null) return "unknown";
	if (entry.expiresAt <= now) return "expired";
	if (entry.expiresAt - now <= 30 * 24 * 60 * 60 * 1e3) return "expiring";
	return "valid";
}

//#endregion
export { addCertificates, areCertificateBodies, getIdpCertificateStatus, haveCertificatesChanged, parseCertificateFile, removeCertificate, toIdpCertificateEntries, toIdpCertificatesParam };
//# sourceMappingURL=idpCertificates.js.map