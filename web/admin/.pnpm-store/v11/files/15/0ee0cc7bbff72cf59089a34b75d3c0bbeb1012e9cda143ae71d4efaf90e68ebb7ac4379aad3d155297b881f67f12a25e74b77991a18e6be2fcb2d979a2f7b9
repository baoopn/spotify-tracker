import { allStrategiesButtonsComparator } from "../../utils/factorSorting.js";
import { factorHasLocalStrategy } from "../SignIn/utils.js";
import { useMemo } from "react";
import { isWebAuthnSupported } from "@clerk/shared/webauthn";

//#region src/components/UserVerification/useReverificationAlternativeStrategies.ts
const firstFactorsAreEqual = (a, b) => {
	if (!a || !b) return false;
	if (a.strategy === "email_code" && b.strategy === "email_code") return a.emailAddressId === b.emailAddressId;
	if (a.strategy === "phone_code" && b.strategy === "phone_code") return a.phoneNumberId === b.phoneNumberId;
	return a.strategy === b.strategy;
};
const secondFactorsAreEqual = (a, b) => {
	if (!a || !b) return false;
	if (a.strategy === "phone_code" && b.strategy === "phone_code") return a.phoneNumberId === b.phoneNumberId;
	return a.strategy === b.strategy;
};
function useReverificationAlternativeStrategies({ filterOutFactor, supportedFirstFactors }) {
	const firstPartyFactors = useMemo(() => supportedFirstFactors ? supportedFirstFactors.filter((f) => !f.strategy.startsWith("oauth_")).filter((factor) => factorHasLocalStrategy(factor)).filter((factor) => !firstFactorsAreEqual(factor, filterOutFactor)).filter((factor) => factor.strategy === "passkey" ? isWebAuthnSupported() : true).sort(allStrategiesButtonsComparator) : [], [supportedFirstFactors, filterOutFactor]);
	return {
		hasAlternativeStrategies: firstPartyFactors.length > 0,
		firstPartyFactors
	};
}

//#endregion
export { secondFactorsAreEqual, useReverificationAlternativeStrategies };
//# sourceMappingURL=useReverificationAlternativeStrategies.js.map