import { defaultAppBranding, type AppBrandingProfile } from '@openpost/query-catalog';
import { workspaceCtx } from '$lib/stores/workspace.svelte';
import { auth } from '$lib/stores/auth';
import { get } from 'svelte/store';

// Branding is intentionally memory-only and scoped to the current identity.
// A previous workspace's logo must never survive a switch, logout or public route.
const state = $state<{
	identity: string;
	active: boolean;
	profile: AppBrandingProfile;
	dark: boolean;
}>({
	identity: '',
	active: false,
	profile: defaultAppBranding,
	dark: false
});
export const appBranding = {
	get profile() {
		const identity = `${get(auth).user?.id ?? ''}:${workspaceCtx.currentWorkspace?.id ?? ''}`;
		return state.active && state.identity === identity ? state.profile : defaultAppBranding;
	},
	get dark() {
		return state.dark;
	},
	title(value: string) {
		return value.replaceAll('SocialFlow.ai', () => this.profile.display_name);
	},
	apply(identity: string, active: boolean, profile: AppBrandingProfile, dark: boolean) {
		state.identity = identity;
		state.active = active;
		state.profile = profile;
		state.dark = dark;
	}
};
