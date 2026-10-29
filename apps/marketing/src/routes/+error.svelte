<script lang="ts">
	import { page } from '$app/state';
	import ErrorRecovery from './_components/ErrorRecovery.svelte';
	import { marketingErrorRecovery } from './_error-recovery';

	const isNotFound = $derived(page.status === 404);
</script>

<svelte:head>
	<title>{isNotFound ? 'Page not found' : 'Something went wrong'} · SocialFlow.ai</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ErrorRecovery
	status={page.status}
	label={isNotFound ? marketingErrorRecovery.label : 'Page error'}
	title={isNotFound ? marketingErrorRecovery.title : 'Page unavailable'}
	description={isNotFound ? marketingErrorRecovery.description : 'Try again, or return home.'}
/>
