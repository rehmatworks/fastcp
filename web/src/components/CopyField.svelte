<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import Eye from '@lucide/svelte/icons/eye';
  import EyeOff from '@lucide/svelte/icons/eye-off';
  import { toast } from '../lib/toast.svelte';

  interface Props {
    value: string;
    label?: string;
    secret?: boolean;
    multiline?: boolean;
  }
  let { value, label, secret = false, multiline = false }: Props = $props();
  let copied = $state(false);
  let revealed = $state(false);
  let box: HTMLElement | undefined = $state();
  const uid = $props.id();

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      // Clipboard API needs a secure context; fall back to selecting the text.
      const sel = window.getSelection();
      if (box && sel) {
        const range = document.createRange();
        range.selectNodeContents(box);
        sel.removeAllRanges();
        sel.addRange(range);
      }
      toast.info('Press Ctrl+C or ⌘C to copy the selected text.');
      return;
    }
    copied = true;
    setTimeout(() => (copied = false), 1500);
  }
</script>

<div class="copy-field">
  {#if label}<span class="label" id="cf-{uid}">{label}</span>{/if}
  <div class="box" class:multiline>
    <code bind:this={box} aria-labelledby={label ? `cf-${uid}` : undefined}>{secret && !revealed ? '•'.repeat(Math.min(value.length, 24)) : value}</code>
    <div class="buttons">
      {#if secret}
        <button type="button" onclick={() => (revealed = !revealed)} aria-label={revealed ? 'Hide' : 'Show'} title={revealed ? 'Hide' : 'Show'}>
          {#if revealed}<EyeOff size={14} strokeWidth={1.75} aria-hidden="true" />{:else}<Eye size={14} strokeWidth={1.75} aria-hidden="true" />{/if}
        </button>
      {/if}
      <button type="button" onclick={copy} aria-label={copied ? 'Copied' : `Copy${label ? ` ${label}` : ''}`} title="Copy">
        {#if copied}<Check size={14} strokeWidth={2} aria-hidden="true" />{:else}<Copy size={14} strokeWidth={1.75} aria-hidden="true" />{/if}
      </button>
    </div>
  </div>
</div>

<style>
  .copy-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .label {
    font-weight: 500;
    font-size: 13px;
  }
  .box {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
    padding: 4px 4px 4px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-subtle);
    min-width: 0;
  }
  .box.multiline {
    align-items: flex-start;
    padding-top: 7px;
  }
  code {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .multiline code {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 160px;
    overflow-y: auto;
  }
  .buttons {
    display: flex;
    gap: 2px;
    flex-shrink: 0;
  }
  .multiline .buttons {
    margin-top: -3px;
  }
  button {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--fg-subtle);
    cursor: pointer;
  }
  button:hover {
    background: var(--bg-emphasis);
    color: var(--fg);
  }
</style>
