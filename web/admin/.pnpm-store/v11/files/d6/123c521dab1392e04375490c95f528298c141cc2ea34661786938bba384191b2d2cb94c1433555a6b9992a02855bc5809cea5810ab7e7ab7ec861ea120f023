import { useInView } from "./useInView.js";
import { organizationListParams } from "../components/OrganizationSwitcher/utils.js";
import { useOrganizationList } from "@clerk/shared/react/index";

//#region src/hooks/useOrganizationListInView.ts
/**
* @internal
*
* `enabled` withholds the list params so the three requests do not start. Defaults on.
*/
const useOrganizationListInView = ({ enabled = true } = {}) => {
	const { userMemberships, userInvitations, userSuggestions } = useOrganizationList(enabled ? organizationListParams : void 0);
	const { ref } = useInView({
		threshold: 0,
		onChange: (inView) => {
			if (!enabled || !inView) return;
			if (userMemberships.hasNextPage) userMemberships.fetchNext?.();
			else if (userInvitations.hasNextPage) userInvitations.fetchNext?.();
			else userSuggestions.fetchNext?.();
		}
	});
	return {
		userMemberships,
		userInvitations,
		userSuggestions,
		ref
	};
};

//#endregion
export { useOrganizationListInView };
//# sourceMappingURL=useOrganizationListInView.js.map