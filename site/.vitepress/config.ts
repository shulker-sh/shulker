import { cpSync, createReadStream, existsSync } from 'node:fs'
import { join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'
import { defineConfig, type MarkdownRenderer } from 'vitepress'
import { transformerTwoslash } from '@shikijs/vitepress-twoslash'
import { groupIconMdPlugin, groupIconVitePlugin } from 'vitepress-plugin-group-icons'
import llmstxt from 'vitepress-plugin-llms'

const siteUrl = 'https://shulker.sh'
const schemaDir = fileURLToPath(new URL('../../schema/v1', import.meta.url))
const repoRoot = fileURLToPath(new URL('../..', import.meta.url))
const installers = ['install.sh', 'install.ps1']

function serveInstallers(): Plugin {
  return {
    name: 'shulker-installers',
    configureServer(server) {
      for (const name of installers) {
        server.middlewares.use(`/${name}`, (req, res) => {
          res.setHeader('Content-Type', 'text/plain; charset=utf-8')
          createReadStream(join(repoRoot, name)).pipe(res)
        })
      }
    },
  }
}

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

function titleWithCopyPage(md: MarkdownRenderer) {
  const rules = md.renderer.rules
  const renderToken: NonNullable<typeof rules.heading_open> = (tokens, idx, options, _env, self) =>
    self.renderToken(tokens, idx, options)
  const open = rules.heading_open ?? renderToken
  const close = rules.heading_close ?? renderToken
  rules.heading_open = (tokens, idx, ...rest) =>
    (tokens[idx].tag === 'h1' ? '<div class="page-title">' : '') + open(tokens, idx, ...rest)
  rules.heading_close = (tokens, idx, ...rest) =>
    close(tokens, idx, ...rest) + (tokens[idx].tag === 'h1' ? '<CopyPage /></div>' : '')
}

export default defineConfig({
  title: 'Shulker',
  description: 'Manage Minecraft mods, client instances, and servers from one manifest',
  cleanUrls: true,
  lastUpdated: true,
  head: [
    ['link', { rel: 'icon', href: '/logo.png', type: 'image/png' }],
    ['meta', { name: 'theme-color', content: '#d9772b' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Shulker' }],
    ['meta', { property: 'og:image', content: `${siteUrl}/logo.png` }],
    ['meta', { property: 'og:image:width', content: '512' }],
    ['meta', { property: 'og:image:height', content: '512' }],
    ['meta', { name: 'twitter:card', content: 'summary' }],
  ],
  sitemap: { hostname: siteUrl },
  transformHead({ pageData, title, description }) {
    const path = pageData.relativePath.replace(/(^|\/)index\.md$/, '$1').replace(/\.md$/, '')
    return [
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: `${siteUrl}/${path}` }],
    ]
  },
  themeConfig: {
    logo: '/logo.png',
    nav: [
      { text: 'Guide', link: '/docs/getting-started' },
      { text: 'CLI', link: '/docs/cli' },
      { text: 'Schema', link: '/docs/manifest' },
      { text: 'Changelog', link: '/docs/changelog' },
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
            { text: 'Instance (.shulker/instance.json)', link: '/docs/instance' },
            { text: 'Changelog', link: '/docs/changelog' },
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
      md.use(titleWithCopyPage)
    },
  },
  vite: {
    plugins: [
      groupIconVitePlugin(),
      serveSchema(),
      serveInstallers(),
      llmstxt({
        domain: siteUrl,
        details: [
          `Validate shulker.json against ${siteUrl}/schema/v1/manifest.json and shulker.lock against ${siteUrl}/schema/v1/lock.json.`,
          'Every command accepts `--json` for machine-readable output and errors.',
        ].join(' '),
      }),
    ],
  },
  buildEnd(site) {
    cpSync(schemaDir, resolve(site.outDir, 'schema/v1'), { recursive: true })
    for (const name of installers) cpSync(join(repoRoot, name), resolve(site.outDir, name))
  },
})
