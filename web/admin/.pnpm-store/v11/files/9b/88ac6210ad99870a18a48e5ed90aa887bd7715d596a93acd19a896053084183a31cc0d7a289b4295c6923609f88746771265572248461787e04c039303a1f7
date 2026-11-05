import type { ClerkPaginatedResponse, DeletedObjectResource, DirectorySyncGroupRoleMappingsResource, DirectorySyncGroupsPage, DirectorySyncJSON, DirectorySyncJSONSnapshot, DirectorySyncProvider, DirectorySyncResource, DirectorySyncStatusResource, DirectorySyncUserJSON, DirectorySyncUserResource, GetDirectorySyncGroupsParams, GetDirectorySyncUsersParams, ReplaceDirectorySyncGroupRoleMappingsParams, SetDirectorySyncCredentialsParams, UpdateDirectorySyncParams } from '@clerk/shared/types';
import { BaseResource } from './Base';
export declare class DirectorySync extends BaseResource implements DirectorySyncResource {
    id: string;
    name: string;
    organizationId: string;
    enterpriseConnectionId: string;
    endpointUrl: string;
    provider: DirectorySyncProvider;
    enabled: boolean;
    groupRoleMappingEnabled: boolean;
    attributeMapping: Record<string, string>;
    credentialsConfigured: boolean | null;
    apiKey: string | null;
    createdAt: Date | null;
    updatedAt: Date | null;
    constructor(data: DirectorySyncJSON | DirectorySyncJSONSnapshot | null, organizationId: string);
    private get directoryPath();
    update: (params: UpdateDirectorySyncParams) => Promise<DirectorySyncResource>;
    rotateToken: () => Promise<DirectorySyncResource>;
    delete: () => Promise<DeletedObjectResource>;
    setCredentials: (params: SetDirectorySyncCredentialsParams) => Promise<DirectorySyncResource>;
    sync: () => Promise<void>;
    getSyncStatus: () => Promise<DirectorySyncStatusResource>;
    getGroups: (params?: GetDirectorySyncGroupsParams) => Promise<DirectorySyncGroupsPage>;
    getGroupRoleMappings: () => Promise<DirectorySyncGroupRoleMappingsResource>;
    replaceGroupRoleMappings: (params: ReplaceDirectorySyncGroupRoleMappingsParams) => Promise<DirectorySyncGroupRoleMappingsResource>;
    getUsers: (params?: GetDirectorySyncUsersParams) => Promise<ClerkPaginatedResponse<DirectorySyncUserResource>>;
    protected fromJSON(data: DirectorySyncJSON | DirectorySyncJSONSnapshot | null): this;
    __internal_toSnapshot(): DirectorySyncJSONSnapshot;
}
export declare class DirectorySyncUser extends BaseResource implements DirectorySyncUserResource {
    id: string;
    userId: string;
    firstName: string | null;
    lastName: string | null;
    identifier: string | null;
    imageUrl: string;
    hasImage: boolean;
    active: boolean;
    provisionedAt: Date | null;
    updatedAt: Date | null;
    constructor(data: DirectorySyncUserJSON | null);
    protected fromJSON(data: DirectorySyncUserJSON | null): this;
}
//# sourceMappingURL=DirectorySync.d.ts.map