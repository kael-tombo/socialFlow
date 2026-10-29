import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { render, cleanup } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import { resolveBuiltInTheme, WebThemeRuntime } from '$lib/themes';
import { QueryClientProvider } from '@tanstack/svelte-query';
import {
	openPostQueryKeys,
	voiceProfileQueryKeys,
	type MediaListItem
} from '@openpost/query-catalog';
import { client } from '$lib/api/client';
import { queryAPI } from '$lib/query/api';
import { mediaQueryAPI } from '$lib/query/media';
import { queryClient } from '$lib/query/client';
import { auth } from '$lib/stores/auth';
import { workspaceCtx } from '$lib/stores/workspace.svelte';
import { userProfileDefaults } from '$lib/test-fixtures/user-profile';
import { ideate } from '$lib/post-builder/ideate.svelte';
import { m } from '$lib/paraglide/messages';
import IdeateDialog from './ideate-dialog.svelte';
import thumbnail from '../../../../../../assets/brand/pwa-192.png?url&no-inline';
import '../../../routes/layout.css';

const image: MediaListItem = {
	id: 'milestone-image',
	workspace_id: 'workspace-ideate',
	original_filename: 'Milestone.png',
	mime_type: 'image/png',
	size: 1024,
	width: 640,
	height: 480,
	url: thumbnail,
	thumbnail_url: '',
	is_favorite: false,
	created_at: '2026-10-21T10:00:00Z',
	usage_count: 0,
	can_delete: true,
	processing_status: 'ready',
	processing_progress: 100,
	analysis_status: 'ready',
	duration_ms: 0,
	frame_rate: 0,
	source: 'upload',
	asset_kind: 'library',
	tags: [],
	alt_text: 'Milestone image',
	audio_channels: 0,
	bit_rate: 0,
	public_url_status: 0,
	retention_class: 'library',
	rotation: 0
};
const mutation = vi.fn();
const get = vi.fn();

beforeEach(() => {
	sessionStorage.clear();
	queryClient.clear();
	auth.setUser({
		...userProfileDefaults,
		id: 'ideate-user',
		email: 'founder@example.com',
		username: 'founder',
		public_profile_enabled: false,
		is_admin: false,
		is_managed: false,
		has_password: true,
		legal_acceptance_required: false,
		email_verified: true,
		created_at: '2026-10-21T10:00:00Z'
	});
	workspaceCtx.currentWorkspace = {
		id: 'workspace-ideate',
		name: 'Founder',
		organization_id: 'org',
		organization_name: 'Founder',
		can_edit: true,
		can_connect: true,
		can_approve: true,
		created_at: '2026-10-21T10:00:00Z',
		avatar_url: '',
		color: '',
		role: 'admin',
		sso_required: false,
		sso_authenticated: true,
		sso_identity_linked: true
	};
	queryClient.setQueryData(openPostQueryKeys.accounts('workspace-ideate'), [
		{ id: 'account-1', platform: 'x', account_username: 'founder', is_active: true }
	]);
	queryClient.setQueryData(openPostQueryKeys.socialSets('workspace-ideate'), []);
	queryClient.setQueryData(voiceProfileQueryKeys.list('workspace-ideate'), []);
	get.mockReset().mockImplementation(async (path: string) => ({
		data: path === '/media' ? { media: [image], total: 1 } : { tags: [], can_edit: true },
		response: new Response()
	}));
	vi.spyOn(client, 'GET').mockImplementation(get);
	vi.spyOn(queryAPI, 'listAccounts').mockImplementation(
		vi
			.fn()
			.mockResolvedValue([
				{ id: 'account-1', platform: 'x', account_username: 'founder', is_active: true }
			])
	);
	vi.spyOn(mediaQueryAPI, 'getMediaMetadata').mockImplementation(
		vi.fn().mockResolvedValue({ media: [image] })
	);
	vi.spyOn(mediaQueryAPI, 'listMedia').mockImplementation(
		vi.fn().mockResolvedValue({ media: [image], total: 1 })
	);

	mutation.mockReset().mockResolvedValue(
		Response.json({
			id: 'build-1',
			workspace_id: 'workspace-ideate',
			state: 'failed',
			phase: 'failed',
			revision: 1,
			error_message: 'Test generation stopped'
		})
	);
	vi.stubGlobal(
		'fetch',
		vi.fn(async (input, init) =>
			String(input).includes('/media/metadata')
				? Response.json({ media: [image] })
				: mutation(input, init)
		)
	);
	ideate.show({
		workspaceId: 'workspace-ideate',
		contextKey: 'composer:milestone',
		input: {
			workspace_id: 'workspace-ideate',
			idea: 'SocialFlow.ai reached 750 stars on GitHub.',
			account_ids: ['account-1'],
			assets: [{ media_id: image.id, role: 'context', may_publish: true }]
		}
	});
});
afterEach(async () => {
	await cleanup();
	ideate.reset();
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	queryClient.clear();
});

it('lets an author reattach a removed post image from the library', async () => {
	const screen = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);
	await screen.getByRole('button', { name: /^Remove / }).click();
	await screen.getByRole('button', { name: m.ideate_attach(), exact: true }).click();
	const picker = screen.getByRole('dialog', { name: m.media_picker_add_media(), exact: true });
	await expect.element(picker).toBeVisible();
	await picker.screenshot({ path: '../../../../../../tmp/ideate-review/media-picker.png' });
	await picker.getByRole('button', { name: 'Select Milestone.png', exact: true }).click();
	await picker.getByRole('button', { name: m.media_picker_add_media(), exact: true }).click();
	await expect.element(picker).not.toBeInTheDocument();
	await expect.element(screen.getByRole('button', { name: 'Remove Milestone.png' })).toBeVisible();
	const permission = screen.getByRole('checkbox', { name: m.ideate_attachment_publishable() });
	await expect.element(permission).toBeChecked();
	await screen.getByRole('button', { name: m.ideate_generate(), exact: true }).click();
	await vi.waitFor(() => expect(mutation).toHaveBeenCalled());
	const request = JSON.parse(mutation.mock.calls[0][1].body);
	expect(request.assets).toEqual([{ media_id: image.id, role: 'context', may_publish: true }]);
});

it('restores a removed post attachment after reload with its existing publishing permission', async () => {
	const screen = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);
	await screen.getByRole('button', { name: /^Remove / }).click();
	await screen.unmount();
	ideate.reset();
	ideate.show({ workspaceId: 'workspace-ideate', contextKey: 'composer:milestone' });
	const reloaded = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);

	await reloaded
		.getByRole('dialog', { name: m.compose_ai_ideate(), exact: true })
		.screenshot({ path: '../../../../../../tmp/ideate-review/restore-after-reload.png' });
	await reloaded
		.getByRole('button', { name: m.compose_ai_media_use_source(), exact: true })
		.click();
	await expect
		.element(reloaded.getByRole('button', { name: 'Remove Milestone.png' }))
		.toBeVisible();
	await expect
		.element(reloaded.getByRole('checkbox', { name: m.ideate_attachment_publishable() }))
		.toBeChecked();
	await expect
		.element(reloaded.getByRole('button', { name: m.compose_ai_media_use_source(), exact: true }))
		.not.toBeInTheDocument();
});

it('keeps topic research failures beside the research action and allows post generation', async () => {
	mutation.mockResolvedValueOnce(
		Response.json({ detail: 'Search is temporarily unavailable.' }, { status: 503 })
	);
	const screen = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);
	await screen.getByRole('button', { name: m.ideate_suggest(), exact: true }).click();
	await expect.element(screen.getByText('Search is temporarily unavailable.')).toBeVisible();
	await expect
		.element(screen.getByRole('button', { name: m.ideate_suggest(), exact: true }))
		.toBeEnabled();
	await expect
		.element(screen.getByRole('button', { name: m.ideate_generate(), exact: true }))
		.toBeEnabled();
	expect(JSON.parse(mutation.mock.calls[0][1].body)).toMatchObject({
		platforms: ['x'],
		focus: 'SocialFlow.ai reached 750 stars on GitHub.'
	});
});

it.each([
	{ width: 1280, scheme: 'light' },
	{ width: 1280, scheme: 'dark' },
	{ width: 390, scheme: 'light' },
	{ width: 390, scheme: 'dark' },
	{ width: 320, scheme: 'light' },
	{ width: 320, scheme: 'dark' }
] as const)(
	'keeps attachments and generation reachable at $width in $scheme',
	async ({ width, scheme }) => {
		await page.viewport(width, 900);
		const runtime = new WebThemeRuntime();
		try {
			await runtime.apply(resolveBuiltInTheme('dither', scheme), document.documentElement);
			const screen = await render(
				IdeateDialog,
				{},
				{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
			);
			await expect
				.element(screen.getByRole('button', { name: 'Remove Milestone.png' }))
				.toBeVisible();
			await screen
				.getByRole('textbox', { name: m.ideate_idea_label(), exact: true })
				.fill(
					"Aaaaand SocialFlow.ai has now reached 750 stars on GitHub! Slow period as we're just getting through tons of bugs and polishing existing features rather than marketing, but happy as our users seem happy!"
				);
			const preview = screen.getByRole('img', { name: 'Milestone image', exact: true }).element();
			if (!(preview instanceof HTMLImageElement))
				throw new Error('Expected the attachment preview');
			await vi.waitFor(() => expect(preview.complete && preview.naturalWidth > 0).toBe(true));

			const dialog = screen.getByRole('dialog', { name: m.compose_ai_ideate(), exact: true });
			const bounds = dialog.element().getBoundingClientRect();
			expect(bounds.left).toBeGreaterThanOrEqual(0);
			expect(bounds.right).toBeLessThanOrEqual(width);
			expect(dialog.element().scrollWidth).toBeLessThanOrEqual(dialog.element().clientWidth);
			const generate = screen.getByRole('button', { name: m.ideate_generate(), exact: true });
			await expect.element(generate).toBeVisible();
			expect(generate.element().getBoundingClientRect().bottom).toBeLessThanOrEqual(900);
			await dialog.screenshot({
				path: `../../../../../../tmp/ideate-review/ideate-${width}-${scheme}.png`
			});
		} finally {
			runtime.clear(document.documentElement);
			await page.viewport(1280, 900);
		}
	}
);

it('retains newly added attachments for recovery after removal and reload', async () => {
	const added = { ...image, id: 'new-image', original_filename: 'New.png' };
	vi.mocked(mediaQueryAPI.listMedia).mockResolvedValue({ media: [added], total: 1 });
	vi.mocked(mediaQueryAPI.getMediaMetadata).mockResolvedValue({ media: [image, added] });
	const screen = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);
	await screen.getByRole('button', { name: m.ideate_attach(), exact: true }).click();
	const picker = screen.getByRole('dialog', { name: m.media_picker_add_media(), exact: true });
	await picker.getByRole('button', { name: 'Select New.png', exact: true }).click();
	await picker.getByRole('button', { name: m.media_picker_add_media(), exact: true }).click();
	await screen.getByRole('button', { name: 'Remove New.png' }).click();
	await screen.unmount();
	ideate.reset();
	ideate.show({ workspaceId: 'workspace-ideate', contextKey: 'composer:milestone' });
	const reloaded = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);
	await reloaded
		.getByRole('button', { name: m.compose_ai_media_use_source(), exact: true })
		.click();
	await expect.element(reloaded.getByRole('button', { name: 'Remove New.png' })).toBeVisible();
	const attachments = reloaded.getByRole('list', { name: m.compose_ai_media() });
	await expect
		.element(attachments.getByRole('listitem').filter({ hasText: 'New.png' }).getByRole('checkbox'))
		.not.toBeChecked();
});

it('discards research that finishes after the idea changes', async () => {
	let finish!: (response: Response) => void;
	mutation.mockImplementationOnce(
		() =>
			new Promise<Response>((resolve) => {
				finish = resolve;
			})
	);
	const screen = await render(
		IdeateDialog,
		{},
		{ wrapper: QueryClientProvider, wrapperProps: { client: queryClient } }
	);
	await screen.getByRole('button', { name: m.ideate_suggest(), exact: true }).click();
	await screen
		.getByRole('textbox', { name: m.ideate_idea_label(), exact: true })
		.fill('A different launch idea.');
	finish(
		Response.json({
			opportunities: [
				{ id: 'old-topic', title: 'Old suggestion', hook: 'Old idea', why_it_fits: 'Old context' }
			]
		})
	);
	await expect
		.element(screen.getByRole('button', { name: m.ideate_suggest(), exact: true }))
		.toBeEnabled();
	await expect.element(screen.getByText('Old suggestion')).not.toBeInTheDocument();
	await expect
		.element(screen.getByRole('textbox', { name: m.ideate_idea_label(), exact: true }))
		.toHaveValue('A different launch idea.');
});
