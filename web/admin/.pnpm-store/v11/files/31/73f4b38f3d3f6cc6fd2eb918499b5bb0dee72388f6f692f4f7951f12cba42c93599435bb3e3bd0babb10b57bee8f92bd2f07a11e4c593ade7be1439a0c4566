import type { AttemptBiometricCredentialParams, BiometricCredentialJSON, BiometricCredentialJSONSnapshot, BiometricCredentialPlatform, BiometricCredentialResource, BiometricCredentialStatus, PrepareBiometricCredentialParams, TrustedDeviceAlgorithm, TrustedDeviceChallengeResource } from '@clerk/shared/types';
import { BaseResource } from './internal';
export declare class BiometricCredential extends BaseResource implements BiometricCredentialResource {
    id: string;
    pathRoot: string;
    platform: BiometricCredentialPlatform;
    appIdentifier: string;
    name: string | null;
    algorithm: TrustedDeviceAlgorithm;
    status: BiometricCredentialStatus;
    lastUsedAt: Date | null;
    revokedAt: Date | null;
    createdAt: Date;
    updatedAt: Date;
    constructor(data: BiometricCredentialJSON | BiometricCredentialJSONSnapshot);
    static list(): Promise<BiometricCredentialResource[]>;
    static prepare(params: PrepareBiometricCredentialParams): Promise<TrustedDeviceChallengeResource>;
    static attempt(params: AttemptBiometricCredentialParams): Promise<BiometricCredentialResource>;
    static revoke(biometricCredentialId: string): Promise<BiometricCredentialResource>;
    protected fromJSON(data: BiometricCredentialJSON | BiometricCredentialJSONSnapshot | null): this;
    __internal_toSnapshot(): BiometricCredentialJSONSnapshot;
}
//# sourceMappingURL=BiometricCredential.d.ts.map