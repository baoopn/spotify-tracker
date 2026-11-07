import { createSubscriber } from 'svelte/reactivity';

/** A reactive "now" that only ticks while something reads it. */
class Clock {
	#subscribe: () => void;

	constructor(intervalMs: number) {
		this.#subscribe = createSubscriber((update) => {
			const id = setInterval(update, intervalMs);
			return () => clearInterval(id);
		});
	}

	get now(): number {
		this.#subscribe();
		return Date.now();
	}
}

/**
 * A reactive "now" that ticks every animation frame while something reads it.
 * The browser pauses frames in background tabs, so it costs nothing there.
 */
class FrameClock {
	#subscribe: () => void;

	constructor() {
		this.#subscribe = createSubscriber((update) => {
			let id = requestAnimationFrame(function frame() {
				update();
				id = requestAnimationFrame(frame);
			});
			return () => cancelAnimationFrame(id);
		});
	}

	get now(): number {
		this.#subscribe();
		return Date.now();
	}
}

/** 1 s ticks, for countdowns (and the progress bar with reduced motion). */
export const secondClock = new Clock(1000);
/** Per-frame ticks, for a smoothly moving progress bar. */
export const frameClock = new FrameClock();
/** 20 s ticks, for relative timestamps. */
export const relativeClock = new Clock(20_000);
