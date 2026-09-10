<script setup lang="ts">
import { VPButton } from 'vitepress/theme'
</script>

<template>
  <main class="home">
    <section class="hero">
      <img class="logo" src="/logo.png" alt="" width="128" height="128" />
      <h1>Shulker</h1>
      <p class="tagline">
        One manifest for your Minecraft mods, client instances, and servers.
      </p>
      <pre class="install"><code>curl -fsSL https://shulker.sh/install.sh | sh</code></pre>
      <div class="actions">
        <VPButton text="Get started" href="/docs/getting-started" />
        <VPButton text="GitHub" href="https://github.com/shulker-sh/shulker" theme="alt" />
      </div>
    </section>
  </main>
</template>

<style scoped>
.home {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 4rem 1.5rem;
  background: var(--vp-c-bg);
  color: var(--vp-c-text-1);
}
.hero {
  max-width: 40rem;
  text-align: center;
}
.logo {
  display: block;
  margin: 0 auto 1.5rem;
  image-rendering: pixelated;
}
h1 {
  font-size: 4rem;
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1;
  background: linear-gradient(120deg, var(--vp-c-brand-1), var(--vp-c-brand-3));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.tagline {
  margin-top: 1rem;
  font-size: 1.25rem;
  color: var(--vp-c-text-2);
}
.install {
  margin: 2rem auto;
  display: inline-block;
  padding: 0.75rem 1.25rem;
  border-radius: 8px;
  background: var(--vp-c-bg-alt);
  border: 1px solid var(--vp-c-divider);
  font-family: var(--vp-font-family-mono);
  font-size: 0.9rem;
}
.actions {
  display: flex;
  justify-content: center;
  gap: 0.75rem;
}
</style>
