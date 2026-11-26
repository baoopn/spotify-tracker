// Helpers for the Player page (the owner's Spotify player).

import {
	Car,
	Cast,
	Gamepad2,
	Laptop,
	MonitorSpeaker,
	Radio,
	Smartphone,
	Speaker,
	Tablet,
	Tv
} from '@lucide/svelte';
import type { Component } from 'svelte';
import type { RepeatState } from './types';

/** The icon for a Spotify Connect device type. */
export function deviceIcon(type: string): Component {
	switch (type.toLowerCase()) {
		case 'computer':
			return Laptop;
		case 'smartphone':
			return Smartphone;
		case 'tablet':
			return Tablet;
		case 'speaker':
			return Speaker;
		case 'tv':
			return Tv;
		case 'castvideo':
		case 'castaudio':
			return Cast;
		case 'automobile':
			return Car;
		case 'gameconsole':
			return Gamepad2;
		case 'avr':
		case 'stb':
		case 'audiodongle':
			return Radio;
		default:
			return MonitorSpeaker;
	}
}

/** A readable device type, e.g. "CastVideo" → "Cast video". */
export function deviceTypeLabel(type: string): string {
	const spaced = type.replace(/([a-z])([A-Z])/g, '$1 $2').toLowerCase();
	if (spaced === 'tv' || spaced === 'avr' || spaced === 'stb') return spaced.toUpperCase();
	return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/** The repeat cycle of the Spotify apps: off → context → track → off. */
export function nextRepeat(r: RepeatState): RepeatState {
	return r === 'off' ? 'context' : r === 'context' ? 'track' : 'off';
}

/** The `disallows` key that blocks switching to repeat mode `to`. */
export function repeatDisallowKey(to: RepeatState): string {
	return to === 'context' ? 'toggling_repeat_context' : 'toggling_repeat_track';
}

export const repeatLabel: Record<RepeatState, string> = {
	off: 'Repeat is off',
	context: 'Repeating the album or playlist',
	track: 'Repeating this track'
};
