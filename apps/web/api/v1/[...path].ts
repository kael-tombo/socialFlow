/**
 * Vercel Edge Function fallback for /api/v1/* (used when the
 * middleware.ts proxy is not available for the detected framework).
 *
 * Reads the same `API_ORIGIN` environment variable as
 * apps/web/middleware.ts. Set it in Vercel → Settings → Environment
 * Variables and redeploy. Falls back to DEFAULT_API_ORIGIN when unset.
 */

export const runtime = 'edge';

const DEFAULT_API_ORIGIN = 'https://preview-955b742f.space-z.ai';

export default async function handler(request: Request): Promise<Response> {
	const env = (globalThis as { process?: { env?: Record<string, string> } })?.process?.env ?? {};
	const configured = env.API_ORIGIN?.replace(/\/+$/, '');
	const origin = configured || DEFAULT_API_ORIGIN;

	const incoming = new URL(request.url);
	const upstreamUrl = origin + incoming.pathname + incoming.search;

	const headers = new Headers(request.headers);
	headers.delete('host');
	headers.delete('connection');
	headers.delete('content-length');

	let body: BodyInit | undefined;
	if (request.method !== 'GET' && request.method !== 'HEAD') {
		const raw = await request.arrayBuffer();
		if (raw.byteLength > 0) body = raw;
	}

	let upstream: Response;
	try {
		upstream = await fetch(upstreamUrl, {
			method: request.method,
			headers,
			body,
			redirect: 'manual'
		});
	} catch (err) {
		return Response.json(
			{
				error: 'bad_gateway',
				detail: err instanceof Error ? err.message : String(err),
				upstream: upstreamUrl
			},
			{ status: 502 }
		);
	}

	const responseHeaders = new Headers();
	const hopByHop = new Set(['content-encoding', 'content-length', 'transfer-encoding', 'connection']);
	upstream.headers.forEach((value, key) => {
		if (!hopByHop.has(key.toLowerCase())) responseHeaders.set(key, value);
	});
	const getSetCookie = (upstream.headers as unknown as { getSetCookie?: () => string[] }).getSetCookie;
	if (typeof getSetCookie === 'function') {
		for (const cookie of getSetCookie.call(upstream.headers)) {
			responseHeaders.append('set-cookie', cookie);
		}
	}

	return new Response(upstream.body, { status: upstream.status, headers: responseHeaders });
}
