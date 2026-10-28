<script lang="ts" generics="T extends string | number">
  import ChevronDown from '@lucide/svelte/icons/chevron-down';

  interface Option {
    value: T;
    label: string;
    disabled?: boolean;
  }

  interface Props {
    label?: string;
    hint?: string;
    value?: T;
    options: Option[];
    disabled?: boolean;
    id?: string;
    hideLabel?: boolean;
    onchange?: (value: T) => void;
  }

  let { label, hint, value = $bindable(), options, disabled = false, id, hideLabel = false, onchange }: Props = $props();
  const uid = $props.id();
  const selectId = $derived(id ?? `sel-${uid}`);
</script>

<div class="field">
  {#if label}<label for={selectId} class:sr-only={hideLabel}>{label}</label>{/if}
  <div class="control">
    <select
      id={selectId}
      bind:value
      {disabled}
      aria-describedby={hint ? `${selectId}-hint` : undefined}
      onchange={() => onchange?.(value as T)}
    >
      {#each options as o (o.value)}
        <option value={o.value} disabled={o.disabled}>{o.label}</option>
      {/each}
    </select>
    <ChevronDown size={15} strokeWidth={1.75} aria-hidden="true" class="chev" />
  </div>
  {#if hint}<p class="msg" id="{selectId}-hint">{hint}</p>{/if}
</div>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  label {
    font-weight: 500;
    font-size: 13px;
  }
  .control {
    position: relative;
    display: flex;
  }
  select {
    appearance: none;
    width: 100%;
    min-height: 34px;
    padding: 6px 32px 6px 10px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    background: var(--bg);
    font-size: 13.5px;
    cursor: pointer;
  }
  select:focus-visible {
    outline: none;
    border-color: var(--fg);
    box-shadow: 0 0 0 1px var(--fg);
  }
  select:disabled {
    color: var(--fg-subtle);
    cursor: not-allowed;
  }
  .control :global(.chev) {
    position: absolute;
    right: 10px;
    top: 50%;
    transform: translateY(-50%);
    pointer-events: none;
    color: var(--fg-subtle);
  }
  .msg {
    font-size: 12.5px;
    color: var(--fg-subtle);
  }
</style>
