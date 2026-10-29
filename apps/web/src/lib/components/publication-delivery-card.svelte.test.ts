import { expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import PublicationDeliveryCard from './publication-delivery-card.svelte';

const baseRendition = {
	id: 'rendition-1',
	platform: 'x',
	target_key: 'x',
	status: 'failed'
};

it('renders the account photo with its platform badge instead of a bare platform icon', async () => {
	const avatarUrl =
		'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" width="10" height="10"%3E%3Crect width="10" height="10" fill="blue"/%3E%3C/svg%3E';
	const screen = await render(PublicationDeliveryCard, {
		rendition: { ...baseRendition },
		destinationLabel: '@openpost',
		accountAvatarUrl: avatarUrl
	});

	const image = screen.container.querySelector('img');
	expect(image?.getAttribute('src')).toBe(avatarUrl);
});

it('prefers the projected destination photo over the explicit avatar fallback', async () => {
	const projectedUrl =
		'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" width="10" height="10"%3E%3Crect width="10" height="10" fill="green"/%3E%3C/svg%3E';
	const fallbackUrl =
		'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" width="10" height="10"%3E%3Crect width="10" height="10" fill="red"/%3E%3C/svg%3E';
	const screen = await render(PublicationDeliveryCard, {
		rendition: {
			...baseRendition,
			account: { platform: 'x', display_name: '@projected', avatar_url: projectedUrl }
		},
		destinationLabel: '@stale',
		accountAvatarUrl: fallbackUrl
	});

	const image = screen.container.querySelector('img');
	expect(image?.getAttribute('src')).toBe(projectedUrl);
	await expect.element(screen.getByText('@projected', { exact: true })).toBeVisible();
	await expect.element(screen.getByText('@stale', { exact: true })).not.toBeInTheDocument();
});

it('renders the projected destination photo without an explicit avatar prop', async () => {
	const projectedUrl =
		'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" width="10" height="10"%3E%3Crect width="10" height="10" fill="green"/%3E%3C/svg%3E';
	const screen = await render(PublicationDeliveryCard, {
		rendition: {
			...baseRendition,
			account: { platform: 'x', display_name: '@projected', avatar_url: projectedUrl }
		},
		destinationLabel: '@projected'
	});

	const image = screen.container.querySelector('img');
	expect(image?.getAttribute('src')).toBe(projectedUrl);
});

it('keeps its audit identity without a photo for an unavailable account', async () => {
	const screen = await render(PublicationDeliveryCard, {
		rendition: { ...baseRendition },
		destinationLabel: '@gone'
	});

	await expect.element(screen.getByText('@gone')).toBeVisible();
	expect(screen.container.querySelector('img')).toBeNull();
});

it('attaches retryable failure evidence and retry to the exact destination', async () => {
	const onRetry = vi.fn();
	const screen = await render(PublicationDeliveryCard, {
		rendition: {
			...baseRendition,
			delivery: {
				target_key: 'x',
				state: 'rejected',
				current_attempt_id: 'attempt-2',
				current_attempt_number: 2,
				current_attempt_created_at: '2026-08-13T10:00:00Z',
				error_kind: 'provider_http',
				error_code: 'rate_limited',
				error_http_status: 429,
				recovery_action: 'retry'
			}
		},
		onRetry
	});

	await expect.element(screen.getByText('Rejected')).toBeVisible();
	await expect.element(screen.getByText(/provider_http.*rate_limited/)).toBeVisible();
	await expect.element(screen.getByText(/Provider attempt 2/)).toBeVisible();
	await screen.getByRole('button', { name: 'Retry destination' }).click();
	expect(onRetry).toHaveBeenCalledWith('rendition-1');
});

it('does not offer a stale retry after the destination has been queued again', async () => {
	const screen = await render(PublicationDeliveryCard, {
		rendition: {
			...baseRendition,
			status: 'scheduled',
			delivery: {
				target_key: 'x',
				current_attempt_id: 'attempt-1',
				current_attempt_number: 1,
				current_attempt_created_at: '2026-08-13T10:00:00Z',
				state: 'rejected',
				recovery_action: 'retry'
			}
		},
		onRetry: vi.fn()
	});

	await expect
		.element(screen.getByRole('button', { name: 'Retry destination' }))
		.not.toBeInTheDocument();
});

it('does not offer replay while an ambiguous attempt is being reconciled', async () => {
	const screen = await render(PublicationDeliveryCard, {
		rendition: {
			...baseRendition,
			delivery: {
				target_key: 'x',
				state: 'ambiguous',
				current_attempt_id: 'attempt-3',
				current_attempt_number: 3,
				current_attempt_created_at: '2026-08-13T10:00:00Z',
				recovery_action: 'reconcile'
			}
		},
		onRetry: vi.fn()
	});

	await expect
		.element(screen.getByText('SocialFlow.ai is checking the provider before another send.'))
		.toBeVisible();
	await expect
		.element(screen.getByRole('button', { name: 'Retry destination' }))
		.not.toBeInTheDocument();
});

it.each([{ resolvable: true }, { resolvable: false }])(
	'offers review for a manual-resolution outcome only when the caller can resolve it',
	async ({ resolvable }) => {
		const onManualResolution = vi.fn();
		const rendition = {
			...baseRendition,
			delivery: {
				target_key: 'x',
				state: 'manual_resolution' as const,
				current_attempt_id: 'attempt-4',
				current_attempt_number: 4,
				current_attempt_created_at: '2026-08-13T10:00:00Z',
				recovery_action: 'manual_resolution' as const
			}
		};
		const screen = resolvable
			? await render(PublicationDeliveryCard, {
					rendition,
					onRetry: vi.fn(),
					onManualResolution
				})
			: await render(PublicationDeliveryCard, { rendition, onRetry: vi.fn() });

		await expect.element(screen.getByText('Manual review required')).toBeVisible();
		const review = screen.getByRole('button', { name: 'Review destination' });
		if (resolvable) {
			await review.click();
			expect(onManualResolution).toHaveBeenCalledWith('rendition-1');
		} else {
			await expect.element(review).not.toBeInTheDocument();
		}
		await expect
			.element(screen.getByRole('button', { name: 'Retry destination' }))
			.not.toBeInTheDocument();
	}
);
