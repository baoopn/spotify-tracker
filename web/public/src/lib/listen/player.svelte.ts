// The listen-along player: one persistent <audio> that follows the owner's
// Spotify playback (see sync.ts for the rules). Listeners can only join, mute
// and set the volume; Bao's position decides everything else.

import { untrack } from 'svelte';
import { ApiError } from '../api';
import { live } from '../live.svelte';
import type { NowPlaying } from '../types';
import { listenAuth } from './auth.svelte';
import { fetchListenAudio } from './api';
import {
	ACCESS_CHECK_MS,
	CHECK_MS,
	DRIFT_MS,
	TRACK_MISMATCH_RETRIES,
	TRACK_MISMATCH_RETRY_MS,
	UNAVAILABLE_RETRY_MS,
	audioNote,
	parseVolume,
	seekTarget,
	shouldRefreshUrl,
	syncAction,
	targetMs
} from './sync.ts';
import {
	MUTED_KEY,
	VOLUME_KEY,
	readStored,
	setMediaMetadata,
	setupMediaSession,
	store
} from './media';

export type ListenStatus =
	| 'waiting'
	| 'loading'
	| 'in-sync'
	| 'paused-by-owner'
	| 'no-audio'
	| 'cant-play'
	| 'ended'
	| 'access-ended'
	/** Listen along was switched off on the server. */
	| 'off';

const ERROR_RETRIES = 3;
const ERROR_BACKOFF_MS = 5000;
const ACCESS_ENDED_NOTE = 'Your access to listen along has ended';
const OFF_NOTE = 'Listen along is off right now';

class ListenPlayer {
	joined = $state(false);
	status = $state<ListenStatus>('waiting');
	note = $state<string | null>(null);
	volume = $state(parseVolume(null));
	muted = $state(false);
	/** False where the browser ignores audio.volume (iOS): hide the slider. */
	volumeSupported = $state(true);
	/** A source with metadata is loaded: Join can play inside the click (iOS). */
	ready = $state(false);

	#audio: HTMLAudioElement | null = null;
	#np: NowPlaying | null = null;
	#enabled = false;
	#accessEnded = false;
	/** Track whose audio is (being) loaded; undefined = none yet. */
	#trackId: string | null | undefined = undefined;
	/** Bumped on every track change, so stale replies are ignored. */
	#gen = 0;
	#expiresAt: number | null = null;
	/** Generation of the request in flight, if any. */
	#fetchingGen: number | null = null;
	#lastSeek = 0;
	#errors = 0;
	/** Pause events we caused ourselves (not the OS or a headset button). */
	#quietPauses = 0;
	#timers = new Set<ReturnType<typeof setTimeout>>();
	#loop: ReturnType<typeof setInterval> | null = null;
	#accessLoop: ReturnType<typeof setInterval> | null = null;

	/**
	 * Attachment for the page's <audio>: wires events and the sync loop.
	 * Untracked: an attachment re-runs (and tears down) when state it read
	 * changes, and this one reads volume and mute.
	 */
	attach = (el: HTMLAudioElement) => untrack(() => this.#attach(el));

	#attach(el: HTMLAudioElement): () => void {
		this.#audio = el;
		this.volume = parseVolume(readStored(VOLUME_KEY));
		this.muted = readStored(MUTED_KEY) === '1';
		const before = el.volume;
		el.volume = 0.5;
		this.volumeSupported = el.volume === 0.5;
		el.volume = this.volume;
		el.muted = this.muted;
		if (!this.volumeSupported) el.volume = before;

		const on = <K extends keyof HTMLMediaElementEventMap>(type: K, fn: () => void) => {
			el.addEventListener(type, fn);
			return () => el.removeEventListener(type, fn);
		};
		const offs = [
			on('loadedmetadata', () => this.#onMetadata()),
			on('playing', () => this.#tick()),
			on('pause', () => this.#onPause()),
			on('ended', () => {
				if (this.joined) this.status = 'ended';
			}),
			on('error', () => this.#onError())
		];
		this.#loop = setInterval(() => this.#tick(), CHECK_MS);
		setupMediaSession(
			() => this.join(),
			() => {
				this.joined = false;
				this.#stopAccessChecks();
				this.#pauseQuietly(el);
			}
		);

		return () => {
			for (const off of offs) off();
			this.#stopAll();
			this.#audio = null;
		};
	}

	/** Called with every now-playing snapshot; `enabled` = signed in and allowed. */
	update(np: NowPlaying | null, enabled: boolean): void {
		this.#np = np;
		if (!enabled) {
			if (this.#enabled) this.#reset();
			this.#enabled = false;
			return;
		}
		this.#enabled = true;
		if (this.#accessEnded) return;
		const id = np?.track?.id ?? null;
		if (id !== this.#trackId) this.#changeTrack(id);
		this.#tick();
	}

	/** The Join button: must call play() synchronously in the click (autoplay rules). */
	join(): void {
		const el = this.#audio;
		// Only with a loaded source, so play() runs inside the click (iOS).
		if (!el || this.#accessEnded || !this.ready || !el.src) return;
		this.joined = true;
		this.#startAccessChecks();
		const target = this.#target();
		if (target != null && el.readyState >= HTMLMediaElement.HAVE_METADATA) {
			el.currentTime = seekTarget(target, el.duration * 1000) / 1000;
			this.#lastSeek = Date.now();
		}
		// Always play in the gesture (unlocks the element on iOS); the next tick
		// pauses again if Bao is paused.
		el.play().then(
			() => this.#tick(),
			(err) => this.#onPlayRejected(err)
		);
	}

	setVolume(v: number): void {
		this.volume = Math.min(1, Math.max(0, v));
		if (this.#audio) this.#audio.volume = this.volume;
		store(VOLUME_KEY, String(this.volume));
		if (this.muted && this.volume > 0) this.toggleMute();
	}

	toggleMute(): void {
		this.muted = !this.muted;
		if (this.#audio) this.#audio.muted = this.muted;
		store(MUTED_KEY, this.muted ? '1' : '0');
	}

	// ---------- track and audio URL ----------

	#target(): number | null {
		const np = this.#np;
		return targetMs(np, live.serverNow(), np?.track?.duration_ms ?? 0);
	}

	#changeTrack(id: string | null): void {
		this.#gen++;
		this.#trackId = id;
		this.#errors = 0;
		this.#clearTimers();
		this.#clearSource();
		setMediaMetadata(this.#np?.track);
		if (!id) {
			this.status = 'waiting';
			this.note = audioNote('idle');
			return;
		}
		this.status = 'loading';
		this.note = null;
		void this.#loadAudio(this.#gen, 0, false);
	}

	/** GET /listen/audio for the current track; `andPlay` resumes after a URL refresh. */
	async #loadAudio(gen: number, attempt: number, andPlay: boolean): Promise<void> {
		const el = this.#audio;
		if (!el || gen !== this.#gen || this.#fetchingGen === gen) return;
		this.#fetchingGen = gen;
		let res;
		try {
			res = await fetchListenAudio(await listenAuth.getToken());
		} catch (err) {
			if (this.#fetchingGen === gen) this.#fetchingGen = null;
			if (gen !== this.#gen) return;
			await this.#onAudioError(err, gen, attempt, andPlay);
			return;
		}
		if (this.#fetchingGen === gen) this.#fetchingGen = null;
		if (gen !== this.#gen || this.#accessEnded) return;

		if (res.track_id !== this.#trackId) {
			// The server still answers for the previous song: ask again shortly.
			if (attempt < TRACK_MISMATCH_RETRIES) {
				this.#later(() => void this.#loadAudio(gen, attempt + 1, andPlay), TRACK_MISMATCH_RETRY_MS);
			} else {
				this.status = 'no-audio';
				this.note = audioNote(res.reason ?? 'unavailable');
			}
			return;
		}
		if (!res.url) {
			this.status = 'no-audio';
			this.note = audioNote(res.reason);
			if (res.reason === 'unavailable') {
				this.#later(() => void this.#loadAudio(gen, 0, false), UNAVAILABLE_RETRY_MS);
			}
			return;
		}
		if (res.content_type && el.canPlayType(res.content_type) === '') {
			this.status = 'cant-play';
			this.note = "This browser can't play this audio";
			return;
		}
		const exp = res.expires_at ? Date.parse(res.expires_at) : NaN;
		this.#expiresAt = Number.isFinite(exp) ? exp : null;
		this.note = null;
		this.status = 'loading';
		this.#pauseQuietly(el);
		this.ready = false;
		el.src = res.url;
		el.load();
		// load() drops queued media events, our pause event included.
		this.#quietPauses = 0;
		if (andPlay && this.joined) this.#play(el);
	}

	async #onAudioError(err: unknown, gen: number, attempt: number, andPlay: boolean) {
		if (err instanceof ApiError && err.status === 403) {
			this.#endAccess();
			return;
		}
		if (err instanceof ApiError && err.status === 404) {
			this.#endAccess(true);
			return;
		}
		if (err instanceof ApiError && err.status === 401) {
			const access = await listenAuth.check();
			if (gen !== this.#gen) return;
			if (access === 'forbidden' || access === 'disabled') {
				this.#endAccess(access === 'disabled');
				return;
			}
			if (access === 'signed-out') return;
			if (access === 'ok' && attempt < 1) {
				void this.#loadAudio(gen, attempt + 1, andPlay);
				return;
			}
			// still refused, or the check failed: try again later
		}
		this.status = 'no-audio';
		this.note = audioNote('unavailable');
		this.#later(() => void this.#loadAudio(gen, 0, andPlay), UNAVAILABLE_RETRY_MS);
	}

	// ---------- sync loop ----------

	#tick(): void {
		const el = this.#audio;
		if (!el || !this.#enabled || this.#accessEnded) return;
		const np = this.#np;
		const target = this.#target();
		const ownerPlaying = !!np?.is_playing && target != null;
		const hasSource = !!el.src;
		// The file ran out: wait for the next song, unless Bao went back.
		if (el.ended && (target == null || target >= el.duration * 1000 - DRIFT_MS)) {
			if (this.joined) this.status = 'ended';
			return;
		}
		const action = syncAction({
			joined: this.joined && hasSource,
			ownerPlaying,
			audioPaused: el.paused,
			audioReady: hasSource && el.readyState >= HTMLMediaElement.HAVE_METADATA,
			audioMs: el.currentTime * 1000,
			targetMs: target,
			audioDurationMs: el.duration * 1000,
			msSinceSeek: Date.now() - this.#lastSeek
		});

		switch (action.kind) {
			case 'pause':
				this.#pauseQuietly(el);
				break;
			case 'ended':
				this.#pauseQuietly(el);
				this.status = 'ended';
				return;
			case 'play':
				if (shouldRefreshUrl(this.#expiresAt, Date.now())) {
					void this.#loadAudio(this.#gen, 0, true);
					return;
				}
				el.currentTime = action.toMs / 1000;
				this.#lastSeek = Date.now();
				this.#play(el);
				break;
			case 'seek':
				el.currentTime = action.toMs / 1000;
				this.#lastSeek = Date.now();
				break;
		}
		this.#updateStatus(el, ownerPlaying, target);
	}

	#updateStatus(el: HTMLAudioElement, ownerPlaying: boolean, target: number | null): void {
		if (this.status === 'no-audio' || this.status === 'cant-play') return;
		if (!this.#trackId || target == null) {
			this.status = 'waiting';
			this.note = this.#trackId ? null : audioNote('idle');
			return;
		}
		if (this.status === 'ended' && el.paused) return;
		if (!ownerPlaying) {
			this.status = 'paused-by-owner';
			return;
		}
		if (!el.src || !this.joined) {
			// Not joined: "in-sync" here means ready to join.
			this.status =
				el.src && el.readyState >= HTMLMediaElement.HAVE_METADATA ? 'in-sync' : 'loading';
			return;
		}
		const drift = Math.abs(el.currentTime * 1000 - target);
		this.status =
			!el.paused && el.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA && drift < DRIFT_MS
				? 'in-sync'
				: 'loading';
	}

	#play(el: HTMLAudioElement): void {
		el.play().catch((err) => this.#onPlayRejected(err));
	}

	#onPlayRejected(err: unknown): void {
		if (err instanceof DOMException && err.name === 'NotAllowedError') {
			this.joined = false;
			this.#stopAccessChecks();
		}
		// AbortError: a new source replaced this one; the next tick retries.
	}

	#pauseQuietly(el: HTMLAudioElement): void {
		if (el.paused) return;
		this.#quietPauses++;
		el.pause();
	}

	#onPause(): void {
		const el = this.#audio;
		if (this.#quietPauses > 0) {
			this.#quietPauses--;
			return;
		}
		if (!el || el.ended) return;
		// Paused by the OS, a headset or the lock screen while Bao plays: leave,
		// and show Join again.
		if (this.joined && this.#np?.is_playing) {
			this.joined = false;
			this.#stopAccessChecks();
		}
	}

	#onMetadata(): void {
		const el = this.#audio;
		this.ready = !!el?.getAttribute('src');
		const target = this.#target();
		if (el && target != null) {
			el.currentTime = seekTarget(target, el.duration * 1000) / 1000;
			this.#lastSeek = Date.now();
		}
		this.#tick();
	}

	#onError(): void {
		const el = this.#audio;
		if (!el?.getAttribute('src') || !this.#trackId) return;
		const gen = this.#gen;
		this.ready = false;
		this.#errors++;
		if (this.#errors > ERROR_RETRIES) {
			this.status = 'no-audio';
			this.note = audioNote('unavailable');
			return;
		}
		this.status = 'loading';
		this.#later(() => void this.#loadAudio(gen, 0, this.joined), ERROR_BACKOFF_MS * this.#errors);
	}

	// ---------- access ----------

	#startAccessChecks(): void {
		if (this.#accessLoop) return;
		this.#accessLoop = setInterval(async () => {
			const res = await listenAuth.check();
			if (res === 'forbidden' || res === 'disabled') this.#endAccess(res === 'disabled');
			else if (res === 'signed-out') this.#reset();
		}, ACCESS_CHECK_MS);
	}

	#stopAccessChecks(): void {
		if (this.#accessLoop) clearInterval(this.#accessLoop);
		this.#accessLoop = null;
	}

	/** Stop for good on this page: access revoked, or (`off`) listen along switched off. */
	#endAccess(off = false): void {
		this.#accessEnded = true;
		this.joined = false;
		this.#gen++;
		this.#stopAccessChecks();
		this.#clearTimers();
		this.#clearSource();
		this.status = off ? 'off' : 'access-ended';
		this.note = off ? OFF_NOTE : ACCESS_ENDED_NOTE;
	}

	// ---------- housekeeping ----------

	#clearSource(): void {
		const el = this.#audio;
		this.#expiresAt = null;
		this.ready = false;
		if (!el) return;
		this.#pauseQuietly(el);
		if (el.getAttribute('src') == null) return;
		el.removeAttribute('src');
		el.load();
		this.#quietPauses = 0;
	}

	#later(fn: () => void, ms: number): void {
		const id = setTimeout(() => {
			this.#timers.delete(id);
			fn();
		}, ms);
		this.#timers.add(id);
	}

	#clearTimers(): void {
		for (const id of this.#timers) clearTimeout(id);
		this.#timers.clear();
	}

	/** Signed out or left the page: back to the start. */
	#reset(): void {
		this.joined = false;
		this.#gen++;
		this.#trackId = undefined;
		this.#accessEnded = false;
		this.#stopAccessChecks();
		this.#clearTimers();
		this.#clearSource();
		this.status = 'waiting';
		this.note = null;
	}

	#stopAll(): void {
		this.#reset();
		this.#enabled = false;
		if (this.#loop) clearInterval(this.#loop);
		this.#loop = null;
	}
}

export const listenPlayer = new ListenPlayer();
