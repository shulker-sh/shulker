import { cpSync, createReadStream, existsSync } from 'node:fs'
import { join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'
import { defineConfig } from 'vitepress'
import { transformerTwoslash } from '@shikijs/vitepress-twoslash'
import { groupIconMdPlugin, groupIconVitePlugin } from 'vitepress-plugin-group-icons'

const schemaDir = fileURLToPath(new URL('../../schema/v1', import.meta.url))

function serveSchema(): Plugin {
  return {
    name: 'shulker-schema',
    configureServer(server) {
      server.middlewares.use('/schema/v1', (req, res, next) => {
        const file = join(schemaDir, decodeURIComponent((req.url ?? '').split('?')[0]))
        if (!file.startsWith(schemaDir + sep) || !file.endsWith('.json') || !existsSync(file)) return next()
        res.setHeader('Content-Type', 'application/schema+json')
        createReadStream(file).pipe(res)
      })
    },
  }
}

export default defineConfig({
  title: 'Shulker',
  description: 'Manage Minecraft mods, client instances, and servers from one manifest',
  cleanUrls: true,
  lastUpdated: true,
  head: [['link', { rel: 'icon', href: '/logo.png', type: 'image/png' }]],
  themeConfig: {
    logo: '/logo.png',
    nav: [
      { text: 'Guide', link: '/docs/getting-started' },
      { text: 'CLI', link: '/docs/cli' },
      { text: 'Schema', link: '/docs/manifest' },
    ],
    sidebar: {
      '/docs/': [
        {
          text: 'Guide',
          items: [
            { text: 'Getting started', link: '/docs/getting-started' },
            { text: 'Concepts', link: '/docs/concepts' },
          ],
        },
        {
          text: 'Reference',
          items: [
            { text: 'CLI', link: '/docs/cli' },
            { text: 'Manifest (shulker.json)', link: '/docs/manifest' },
            { text: 'Lock (shulker.lock.json)', link: '/docs/lock' },
          ],
        },
      ],
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/shulker-sh/shulker' }],
    search: { provider: 'local' },
    editLink: {
      pattern: 'https://github.com/shulker-sh/shulker/edit/master/site/:path',
    },
  },
  markdown: {
    theme: { light: 'github-light-default', dark: 'github-dark-default' },
    codeTransformers: [transformerTwoslash()],
    config(md) {
      md.use(groupIconMdPlugin)
    },
  },
  vite: {
    plugins: [groupIconVitePlugin(), serveSchema()],
  },
  buildEnd(site) {
    cpSync(schemaDir, resolve(site.outDir, 'schema/v1'), { recursive: true })
  },
})
