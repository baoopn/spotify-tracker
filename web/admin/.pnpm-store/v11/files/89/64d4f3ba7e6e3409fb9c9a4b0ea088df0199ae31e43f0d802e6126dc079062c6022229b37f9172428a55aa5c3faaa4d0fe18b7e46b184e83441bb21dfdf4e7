import type { AuthConfigJSON, AuthConfigJSONSnapshot, AuthConfigResource, NativeSettingsResource, PhoneCodeChannel } from '@clerk/shared/types';
import { BaseResource } from './internal';
export declare class AuthConfig extends BaseResource implements AuthConfigResource {
    claimedAt: Date | null;
    reverification: boolean;
    singleSessionMode: boolean;
    preferredChannels: Record<string, PhoneCodeChannel> | null;
    sessionMinter: boolean;
    nativeSettings: NativeSettingsResource | null;
    constructor(data?: Partial<AuthConfigJSON> | null);
    protected fromJSON(data: Partial<AuthConfigJSON> | null): this;
    __internal_toSnapshot(): AuthConfigJSONSnapshot;
}
//# sourceMappingURL=AuthConfig.d.ts.map