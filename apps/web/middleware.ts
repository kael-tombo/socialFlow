/*
 * Vercel Edge Middleware — API proxy for split deployments.
 *
 * When the web SPA is hosted on Vercel but the backend (apps/server:
 * API + MCP + jobs + SQLite) runs elsewhere, this middleware proxies
 * same-origin /api/v1/* requests to the backend origin.
 *
 * Configuration:
 *   Set the `API_ORIGIN` environment variable in Vercel
 *   (Project → Settings → Environment Variables) to the backend base
 *   URL, e.g. `API_ORIGIN=https://preview-955b742f.space-z.ai`
 *   (no trailing slash), then redeploy.
 *
 *   If `API_ORIGIN` is unset, DEFAULT_API_ORIGIN below is used so test
 *   deployments keep working without extra setup.
 *
 * A vercel.json rewrite exists as a static fallback; this middleware
 * takes precedence whenever it is deployed, and honors the env var.
 */

const DEFAULT_API_ORIGIN = 'https://preview-955b742f.space-z.ai';

export const config = {
	matcher: '/api/v1/:path*'
};

export default async function middleware(request: Request): Promise<Response> {
	const env = (globalThis as { process?: { env?: Record<string, string> } })?.process?.env ?? {};
	const configured = env.API_ORIGIN?.replace(/\/+$/, '');
	const origin = configured || DEFAULT_API_ORIGIN;
	return proxyToOrigin(request, origin);
}

async function proxyToOrigin(request: Request, origin: string): Promise<Response> {
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
