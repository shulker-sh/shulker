export function onRequestGet({ request, env }) {
    const userAgent = request.headers.get('User-Agent') ?? ''

    if (!userAgent.startsWith('shulker/') || request.headers.get('X-Shulker-Client') !== 'cli') {
        return new Response('Not found', { status: 404 })
    }

    return Response.json({ key: env.CURSEFORGE_KEY }, { headers: { 'Cache-Control': 'no-store' } })
}
