import type { AddSSOBypassAllowlistUserParams, AddSSOBypassAllowlistUsersParams, DeletedObjectResource, SSOBypassAllowlistBulkCreateResult, SSOBypassAllowlistResource, SSOBypassAllowlistUserResource } from '@clerk/shared/types';
export declare class SSOBypassAllowlist implements SSOBypassAllowlistResource {
    private readonly organization;
    constructor(organization: {
        id: string;
    });
    private get path();
    getUsers: () => Promise<SSOBypassAllowlistUserResource[]>;
    addUser: (params: AddSSOBypassAllowlistUserParams) => Promise<SSOBypassAllowlistUserResource>;
    addUsers: (params: AddSSOBypassAllowlistUsersParams) => Promise<SSOBypassAllowlistBulkCreateResult>;
    removeUser: (userId: string) => Promise<DeletedObjectResource>;
}
//# sourceMappingURL=SSOBypassAllowlist.d.ts.map