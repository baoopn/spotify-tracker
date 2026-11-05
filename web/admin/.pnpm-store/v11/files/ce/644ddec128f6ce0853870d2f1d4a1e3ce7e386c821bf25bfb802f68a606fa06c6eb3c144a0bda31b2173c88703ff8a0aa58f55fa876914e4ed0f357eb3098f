import type { ProtectConfigJSON, ProtectConfigJSONSnapshot, ProtectConfigResource, ProtectLoader } from '@clerk/shared/types';
import { BaseResource } from './internal';
export declare class ProtectConfig extends BaseResource implements ProtectConfigResource {
    id: string;
    loaders?: ProtectLoader[];
    tokens_invalid_before?: number;
    challenge_load_timeout_ms?: number;
    rollout?: number;
    constructor(data?: ProtectConfigJSON | ProtectConfigJSONSnapshot | null);
    protected fromJSON(data: ProtectConfigJSON | ProtectConfigJSONSnapshot | null): this;
    __internal_toSnapshot(): ProtectConfigJSONSnapshot;
}
//# sourceMappingURL=ProtectConfig.d.ts.map