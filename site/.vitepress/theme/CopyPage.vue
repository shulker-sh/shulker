<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useCopyOrDownloadAsMarkdownButtons } from 'vitepress-plugin-llms/vitepress-components'
import iconChatGPT from './icons/chatgpt.svg?raw'
import iconCheck from './icons/check.svg?raw'
import iconChevron from './icons/chevron.svg?raw'
import iconClaude from './icons/claude.svg?raw'
import iconCopy from './icons/copy.svg?raw'
import iconDownload from './icons/download.svg?raw'
import iconDownloadTray from './icons/download-tray.svg?raw'
import iconExternal from './icons/external.svg?raw'
import iconMarkdown from './icons/markdown.svg?raw'

const { aiProviders, copied, copyAsMarkdown, downloadMarkdown, openInAI, viewAsMarkdown } =
  useCopyOrDownloadAsMarkdownButtons()
const providerIcons: Record<string, string> = { ChatGPT: iconChatGPT, Claude: iconClaude }

const open = ref(false)
const root = ref<HTMLElement>()

function choose(action: () => unknown) {
  action()
  open.value = false
}

function onClick(event: MouseEvent) {
  if (root.value && !root.value.contains(event.target as Node)) open.value = false
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('click', onClick)
  document.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  document.removeEventListener('click', onClick)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="root" class="copy-page">
    <div class="trigger">
      <button class="copy" @click="copyAsMarkdown">
        <span class="icon" v-html="copied ? iconCheck : iconCopy" />
        {{ copied ? 'Copied' : 'Copy page' }}
      </button>
      <button class="toggle" aria-label="More page actions" :aria-expanded="open" @click="open = !open">
        <span class="icon chevron" :class="{ open }" v-html="iconChevron" />
      </button>
    </div>
    <Transition name="menu">
      <div v-if="open" class="menu">
        <button @click="choose(viewAsMarkdown)">
          <span class="icon" v-html="iconMarkdown" />
          View as Markdown
          <span class="icon trailing" v-html="iconExternal" />
        </button>
        <button @click="choose(downloadMarkdown)">
          <span class="icon" v-html="iconDownload" />
          Download Markdown
          <span class="icon trailing" v-html="iconDownloadTray" />
        </button>
        <button v-for="provider in aiProviders" :key="provider.name" @click="choose(() => openInAI(provider))">
          <span class="icon" v-html="providerIcons[provider.name] ?? iconExternal" />
          Open in {{ provider.name }}
          <span class="icon trailing" v-html="iconExternal" />
        </button>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.copy-page {
  position: relative;
  flex-shrink: 0;
}
.trigger {
  display: flex;
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  overflow: hidden;
  font-size: 14px;
}
.trigger:hover {
  border-color: var(--vp-c-brand-1);
}
.trigger button {
  display: flex;
  align-items: center;
  color: var(--vp-c-text-1);
}
.trigger button:hover {
  background: var(--vp-c-bg-soft);
}
.copy {
  gap: 8px;
  padding: 6px 12px;
  white-space: nowrap;
}
.toggle {
  padding: 0 10px;
  border-left: 1px solid var(--vp-c-divider);
}
.menu {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  z-index: 100;
  min-width: 240px;
  overflow: hidden;
  background: var(--vp-c-bg-elv);
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  box-shadow: var(--vp-shadow-3);
}
.menu button {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 16px;
  color: var(--vp-c-text-1);
  font-size: 14px;
  text-align: left;
}
.menu button::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 0;
  background: var(--vp-c-brand-1);
}
.menu button:hover {
  padding-left: 20px;
  background: var(--vp-c-bg-soft);
}
.menu button:hover::before {
  width: 3px;
}
.icon {
  display: inline-flex;
  width: 18px;
  height: 18px;
}
.icon :deep(svg) {
  width: 100%;
  height: 100%;
}
.trailing {
  margin-left: auto;
  opacity: 0.6;
}
.menu button:hover .trailing {
  opacity: 1;
  transform: translateX(2px);
}
.chevron.open {
  transform: rotate(180deg);
}
.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
@media (prefers-reduced-motion: no-preference) {
  .trigger,
  .trigger button,
  .menu button,
  .menu button::before,
  .trailing,
  .chevron {
    transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  }
  .trigger:hover {
    transform: translateY(-1px);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  }
  .menu-enter-active,
  .menu-leave-active {
    transition: opacity 0.15s, transform 0.15s;
  }
}
</style>
