<!-- A numeric limit where -1 means unlimited. -->
<script lang="ts">
  import Input from '../../components/Input.svelte';
  import Checkbox from '../../components/Checkbox.svelte';

  interface Props {
    label: string;
    value: number;
    unit: string;
    min: number;
    max?: number;
    fallback: number;
    hint?: string;
    error?: string | null;
  }
  let { label, value = $bindable(), unit, min, max, fallback, hint, error }: Props = $props();

  const unlimited = $derived(value === -1);
  let last = $state<number | null>(null);
</script>

<div class="limit">
  <Input
    {label}
    type="number"
    value={unlimited ? '' : value}
    oninput={(e) => {
      const raw = (e.currentTarget as HTMLInputElement).value;
      value = raw === '' ? Number.NaN : Number(raw);
    }}
    placeholder={unlimited ? 'Unlimited' : ''}
    disabled={unlimited}
    {min}
    {max}
    suffix={unit}
    {hint}
    {error}
  />
  <Checkbox
    label="Unlimited"
    checked={unlimited}
    onchange={(on) => {
      if (on) {
        last = value;
        value = -1;
      } else {
        value = last !== null && last >= min ? last : fallback;
      }
    }}
  />
</div>

<style>
  .limit {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
</style>
