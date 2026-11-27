// "Listening now" for the admin: /listen tells the server what this listener
// is doing (POST /listen/presence). The pure rule is tested by
// scripts/check-listen.mjs; PresencePinger sends on change and every 30 s.

import type { ListenPresence, PresenceState } from './types';

/** Re-send this often while the tab is visible (the server keeps it 90 s). */
export const PRESENCE_MS = 30_000;

export type PresenceInput = {
	joined: boolean;
	/** The player's status (see player.svelte.ts). */
	status: string;
	hidden: boolean;
};

/** listening = joined and in sync; idle = tab hidden or the song ended; else following. */
export function presenceState(p: PresenceInput): PresenceState {
	if (p.hidden || p.status === 'ended') return 'idle';
	if (p.joined && p.status === 'in-sync') return 'listening';
	return 'following';
}

export type SendPresence = (body: ListenPresence, keepalive: boolean) => Promise<void>;

export class PresencePinger {
	#send: SendPresence;
	#current: ListenPresence | null = null;
	#sentKey = '';
	#timer: ReturnType<typeof setInterval> | null = null;

	constructor(send: SendPresence) {
		this.#send = send;
	}

	/** Start the 30 s ping and the page-hide handlers. Returns a cleanup function. */
	start(): () => void {
		const onVisibility = () => {
			if (document.visibilityState === 'hidden') this.#hide(false);
			else this.#push(true);
		};
		const onHide = () => this.#hide(true);
		document.addEventListener('visibilitychange', onVisibility);
		window.addEventListener('pagehide', onHide);
		this.#timer = setInterval(() => {
			if (document.visibilityState === 'visible') this.#push(true);
		}, PRESENCE_MS);
		return () => {
			document.removeEventListener('visibilitychange', onVisibility);
			window.removeEventListener('pagehide', onHide);
			if (this.#timer) clearInterval(this.#timer);
			this.#timer = null;
			this.stop();
		};
	}

	/** The latest state; sends at once when it changed. */
	update(next: ListenPresence): void {
		this.#current = next;
		// While hidden the server already has "idle"; the next show sends this.
		if (document.visibilityState === 'hidden') return;
		this.#push(false);
	}

	/** Not allowed any more (signed out, access ended): say idle once, then stay quiet. */
	stop(): void {
		if (this.#current && this.#sentKey && !this.#sentKey.startsWith('idle')) {
			this.#post({ state: 'idle', track_id: this.#current.track_id }, true);
		}
		this.#current = null;
		this.#sentKey = '';
	}

	#hide(keepalive: boolean): void {
		const cur = this.#current;
		if (!cur || this.#sentKey.startsWith('idle')) return;
		this.#post({ state: 'idle', track_id: cur.track_id }, keepalive);
	}

	#push(force: boolean): void {
		const cur = this.#current;
		if (!cur) return;
		if (!force && `${cur.state}|${cur.track_id}` === this.#sentKey) return;
		this.#post(cur, false);
	}

	#post(body: ListenPresence, keepalive: boolean): void {
		this.#sentKey = `${body.state}|${body.track_id}`;
		void this.#send(body, keepalive).catch(() => {});
	}
}

const TRACK_ID = /^[0-9A-Za-z]{22}$/;

/** The server only takes Spotify track ids (local files have none). */
export function presenceTrackId(id: string | null | undefined): string | null {
	return id && TRACK_ID.test(id) ? id : null;
}
