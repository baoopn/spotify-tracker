import type { SignInResource, SignUpResource } from '@clerk/shared/types';
import type { Clerk } from './resources/internal';
/**
 * Resolves a pending `protect_check` on a sign-in or sign-up resource by opening Clerk's Protect
 * modal and waiting for the challenge to clear. One resolution runs at a time. A call on the same
 * resource shares it, and a call on another resource waits for it before resolving its own gate.
 * Skips the proof submission, which belongs to whatever runs the challenge, and no-RHC builds where
 * the challenge script must not be loaded.
 */
export declare class ProtectCheckGate {
    private static instance;
    private inflight;
    static getInstance(): ProtectCheckGate;
    resolve(clerk: Clerk, resource: SignInResource | SignUpResource, action?: string): Promise<void>;
}
//# sourceMappingURL=protectCheckGate.d.ts.map