<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    checked?: boolean;
    label: string;
    description?: string | Snippet;
    disabled?: boolean;
    hideLabel?: boolean;
    /** Makes the switch controlled: it only flips once the parent updates `checked`. */
    onchange?: (checked: boolean) => void;
  }

  let { checked = $bindable(false), label, description, disabled = false, hideLabel = false, onchange }: Props = $props();
  const uid = $props.id();
</script>

<div class="switch-row" class:compact={hideLabel}>
  <button
    type="button"
    role="switch"
    id="sw-{uid}"
    aria-checked={checked}
    aria-label={hideLabel ? label : undefined}
    aria-labelledby={hideLabel ? undefined : `sw-${uid}-label`}
    aria-describedby={description ? `sw-${uid}-desc` : undefined}
    {disabled}
    class="track"
    onclick={() => {
      if (onchange) onchange(!checked);
      else checked = !checked;
    }}
  >
    <span class="thumb"></span>
  </button>
  {#if !hideLabel}
    <div class="text">
      <label id="sw-{uid}-label" for="sw-{uid}">{label}</label>
      {#if description}
        <p id="sw-{uid}-desc" class="desc">
          {#if typeof description === 'string'}{description}{:else}{@render description()}{/if}
        </p>
      {/if}
    </div>
  {/if}
</div>

<style>
  .switch-row {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .switch-row.compact {
    display: inline-flex;
  }
  .track {
    position: relative;
    flex-shrink: 0;
    width: 36px;
    height: 20px;
    margin-top: 1px;
    border-radius: 999px;
    border: 1px solid var(--border-strong);
    background: var(--bg-emphasis);
    cursor: pointer;
    padding: 0;
    transition: background 0.15s var(--ease);
  }
  .track[aria-checked='true'] {
    background: var(--bg-inverse);
    border-color: var(--bg-inverse);
  }
  .track:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--bg);
    box-shadow: var(--shadow-sm);
    transition: transform 0.15s var(--ease);
  }
  .track[aria-checked='false'] .thumb {
    background: var(--fg-subtle);
  }
  .track[aria-checked='true'] .thumb {
    transform: translateX(16px);
    background: var(--fg-inverse);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  label {
    font-weight: 500;
    font-size: 13.5px;
    cursor: pointer;
  }
  .desc {
    color: var(--fg-subtle);
    font-size: 12.5px;
  }
</style>
