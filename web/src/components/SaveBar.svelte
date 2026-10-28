<!-- Save and discard controls for a form section, with an unsaved marker. -->
<script lang="ts">
  import Button from './Button.svelte';

  interface Props {
    dirty: boolean;
    saving: boolean;
    form: string;
    onreset: () => void;
    label?: string;
    disabled?: boolean;
  }
  let { dirty, saving, form, onreset, label = 'Save changes', disabled = false }: Props = $props();
</script>

<div class="save-bar">
  <span class="state" aria-live="polite">{dirty ? 'Unsaved changes' : ''}</span>
  <Button onclick={onreset} disabled={!dirty || saving}>Discard</Button>
  <Button type="submit" {form} variant="primary" loading={saving} disabled={!dirty || disabled}>{label}</Button>
</div>

<style>
  .save-bar {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    width: 100%;
    flex-wrap: wrap;
  }
  .state {
    margin-right: auto;
    font-size: 12.5px;
    font-weight: 500;
    color: var(--fg-muted);
  }
  .state:not(:empty)::before {
    content: '●';
    margin-right: 6px;
    font-size: 9px;
    vertical-align: 1px;
  }
</style>
