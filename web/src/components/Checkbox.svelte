<script lang="ts">
  interface Props {
    checked?: boolean;
    label: string;
    description?: string;
    disabled?: boolean;
    onchange?: (checked: boolean) => void;
  }
  let { checked = $bindable(false), label, description, disabled = false, onchange }: Props = $props();
</script>

<label class="check" class:disabled>
  <input type="checkbox" bind:checked {disabled} onchange={() => onchange?.(checked)} />
  <span class="text">
    <span class="label">{label}</span>
    {#if description}<span class="desc">{description}</span>{/if}
  </span>
</label>

<style>
  .check {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    cursor: pointer;
  }
  .check.disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }
  input {
    appearance: none;
    flex-shrink: 0;
    width: 16px;
    height: 16px;
    margin: 2px 0 0;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--bg);
    display: grid;
    place-content: center;
    cursor: inherit;
  }
  input:checked {
    background: var(--bg-inverse);
    border-color: var(--bg-inverse);
  }
  input:checked::after {
    content: '';
    width: 9px;
    height: 5px;
    border-left: 2px solid var(--fg-inverse);
    border-bottom: 2px solid var(--fg-inverse);
    transform: translateY(-1px) rotate(-45deg);
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .label {
    font-size: 13.5px;
    overflow-wrap: anywhere;
  }
  .desc {
    color: var(--fg-subtle);
    font-size: 12.5px;
  }
</style>
