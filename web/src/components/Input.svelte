<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLInputAttributes } from 'svelte/elements';

  interface Props extends Omit<HTMLInputAttributes, 'value' | 'prefix'> {
    label?: string;
    hint?: string | Snippet;
    error?: string | null;
    value?: string | number | null;
    multiline?: boolean;
    rows?: number;
    mono?: boolean;
    prefix?: string;
    suffix?: string;
    hideLabel?: boolean;
    trailing?: Snippet;
    input?: HTMLInputElement | HTMLTextAreaElement | null;
  }

  let {
    label,
    hint,
    error = null,
    value = $bindable(),
    multiline = false,
    rows = 4,
    mono = false,
    prefix,
    suffix,
    hideLabel = false,
    trailing,
    id,
    class: cls = '',
    input = $bindable(null),
    ...rest
  }: Props = $props();

  const uid = $props.id();
  const inputId = $derived(id ?? `in-${uid}`);
  const hintId = $derived(`${inputId}-hint`);
</script>

<div class="field {cls}">
  {#if label}
    <label for={inputId} class:sr-only={hideLabel}>{label}</label>
  {/if}
  <div class="control" class:invalid={!!error} class:mono class:has-prefix={!!prefix} class:has-suffix={!!suffix || !!trailing}>
    {#if prefix}<span class="affix prefix">{prefix}</span>{/if}
    {#if multiline}
      <textarea
        id={inputId}
        bind:value
        bind:this={input}
        {rows}
        aria-invalid={error ? true : undefined}
        aria-describedby={hint || error ? hintId : undefined}
        placeholder={rest.placeholder}
        disabled={rest.disabled}
        readonly={rest.readonly}
        required={rest.required}
        spellcheck={mono ? false : undefined}
        autocomplete={rest.autocomplete as never}
        maxlength={rest.maxlength}
      ></textarea>
    {:else}
      <input
        id={inputId}
        bind:value
        bind:this={input}
        aria-invalid={error ? true : undefined}
        aria-describedby={hint || error ? hintId : undefined}
        spellcheck={mono ? false : undefined}
        {...rest}
      />
    {/if}
    {#if suffix}<span class="affix suffix">{suffix}</span>{/if}
    {#if trailing}<span class="trailing">{@render trailing()}</span>{/if}
  </div>
  {#if error}
    <p class="msg error" id={hintId}>{error}</p>
  {:else if hint}
    <p class="msg" id={hintId}>
      {#if typeof hint === 'string'}{hint}{:else}{@render hint()}{/if}
    </p>
  {/if}
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
    display: flex;
    align-items: stretch;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    background: var(--bg);
    transition: border-color 0.12s;
    min-width: 0;
  }
  .control:focus-within {
    border-color: var(--fg);
    box-shadow: 0 0 0 1px var(--fg);
  }
  .control.invalid {
    border-color: var(--fg);
    border-style: dashed;
  }
  input,
  textarea {
    flex: 1;
    min-width: 0;
    border: 0;
    background: transparent;
    padding: 7px 10px;
    min-height: 34px;
    outline: none;
    font-size: 13.5px;
  }
  textarea {
    resize: vertical;
    line-height: 1.5;
  }
  .mono input,
  .mono textarea {
    font-family: var(--mono);
    font-size: 12.5px;
  }
  input:disabled,
  textarea:disabled {
    color: var(--fg-subtle);
    cursor: not-allowed;
  }
  input::placeholder,
  textarea::placeholder {
    color: var(--fg-faint);
  }
  .affix {
    display: flex;
    align-items: center;
    padding: 0 10px;
    color: var(--fg-subtle);
    background: var(--bg-subtle);
    font-size: 13px;
    white-space: nowrap;
  }
  .prefix {
    border-right: 1px solid var(--border);
    border-radius: var(--radius) 0 0 var(--radius);
  }
  .suffix {
    border-left: 1px solid var(--border);
    border-radius: 0 var(--radius) var(--radius) 0;
  }
  .trailing {
    display: flex;
    align-items: center;
    padding-right: 4px;
  }
  .msg {
    font-size: 12.5px;
    color: var(--fg-subtle);
  }
  .msg.error {
    color: var(--fg);
    font-weight: 500;
  }
  .msg.error::before {
    content: '! ';
    font-weight: 700;
  }
</style>
