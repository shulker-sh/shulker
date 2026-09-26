import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import TwoslashFloatingVue from '@shikijs/vitepress-twoslash/client'
import CopyPage from './CopyPage.vue'
import '@shikijs/vitepress-twoslash/style.css'
import 'virtual:group-icons.css'
import './style.css'

export default {
    extends: DefaultTheme,
    enhanceApp({ app }) {
        app.use(TwoslashFloatingVue)
        app.component('CopyPage', CopyPage)
    },
} satisfies Theme
