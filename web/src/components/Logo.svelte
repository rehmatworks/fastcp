<script lang="ts">
  let { size = 24, wordmark = true }: { size?: number; wordmark?: boolean } = $props();
</script>

<span class="logo">
  <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
    <rect width="32" height="32" rx="7" class="mark-bg" />
    <path d="M10 8h13v3.5h-9V15h7.5v3.5H14V24h-4z" class="mark-fg" />
  </svg>
  {#if wordmark}<span class="word">FastCP</span>{/if}
</span>

<style>
  .logo {
    display: inline-flex;
    align-items: center;
    gap: 9px;
  }
  .mark-bg {
    fill: var(--fg);
  }
  .mark-fg {
    fill: var(--bg);
  }
  .word {
    font-weight: 650;
    font-size: 15px;
    letter-spacing: -0.02em;
  }
</style>
