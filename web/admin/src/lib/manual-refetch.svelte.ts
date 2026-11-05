/**
 * Tracks refetches the user asked for (Refresh button, ErrorState retry), so
 * views can dim / spin for those only and stay still during background polls.
 */
export class ManualRefetch {
	#inFlight = $state(0);
	#refetch: () => Promise<unknown>;

	constructor(refetch: () => Promise<unknown>) {
		this.#refetch = refetch;
	}

	/** True while a user-triggered refetch is in flight. */
	get active(): boolean {
		return this.#inFlight > 0;
	}

	run = async (): Promise<void> => {
		this.#inFlight++;
		try {
			await this.#refetch();
		} finally {
			this.#inFlight--;
		}
	};
}
