import { defineConfig } from 'vitepress'
import { transformerTwoslash } from '@shikijs/vitepress-twoslash'
import { groupIconMdPlugin, groupIconVitePlugin } from 'vitepress-plugin-group-icons'
import gruvboxLight from '@shikijs/themes/gruvbox-light-medium'
import gruvboxDark from '@shikijs/themes/gruvbox-dark-medium'

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
      pattern: 'https://github.com/shulker-sh/shulker/edit/main/site/:path',
    },
  },
  markdown: {
    theme: { light: gruvboxLight, dark: gruvboxDark },
    codeTransformers: [transformerTwoslash()],
    config(md) {
      md.use(groupIconMdPlugin)
    },
  },
  vite: {
    plugins: [groupIconVitePlugin()],
  },
})
