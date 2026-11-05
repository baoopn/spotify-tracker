import { untrack } from 'svelte';
import { errorMessage } from './api';

/**
 * Minimal async resource: tracks data / error / loading and aborts the
 * previous request when a new one starts. Previous data is kept while
 * refetching so views can dim instead of flashing skeletons.
 */
export class Query<T> {
	data = $state.raw<T | undefined>(undefined);
	error = $state<string | null>(null);
	loading = $state(false);
	#ctrl: AbortController | null = null;
	#last: ((signal: AbortSignal) => Promise<T>) | null = null;

	/** True only for the very first load (nothing to show yet). */
	get initialLoading(): boolean {
		return this.loading && this.data === undefined;
	}

	run(fetcher: (signal: AbortSignal) => Promise<T>): Promise<void> {
		return untrack(() => this.#run(fetcher));
	}

	refetch(): Promise<void> {
		return this.#last ? this.run(this.#last) : Promise.resolve();
	}

	abort(): void {
		this.#ctrl?.abort();
	}

	async #run(fetcher: (signal: AbortSignal) => Promise<T>): Promise<void> {
		this.#ctrl?.abort();
		const ctrl = new AbortController();
		this.#ctrl = ctrl;
		this.#last = fetcher;
		this.loading = true;
		try {
			const data = await fetcher(ctrl.signal);
			if (ctrl.signal.aborted) return;
			this.data = data;
			this.error = null;
		} catch (err) {
			if (ctrl.signal.aborted) return;
			this.error = errorMessage(err);
		} finally {
			if (this.#ctrl === ctrl) this.loading = false;
		}
	}
}
