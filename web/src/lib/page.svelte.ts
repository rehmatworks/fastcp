// Helpers shared by pages: data loading tied to the component lifetime and
// form state with unsaved-change tracking.

import { untrack } from 'svelte';
import { ApiError, isAbort, errorMessage } from './api';
import { router } from './router.svelte';

/**
 * Loads data while the calling component is mounted. The request is aborted
 * when the component is destroyed. Call reload() after mutations.
 */
export class Resource<T> {
  data = $state<T | undefined>(undefined);
  error = $state<string | null>(null);
  /** HTTP status of the last failure, or 0 for network errors. */
  status = $state(0);
  loading = $state(true);
  #load: (signal: AbortSignal) => Promise<T>;
  #controller: AbortController | null = null;

  constructor(load: (signal: AbortSignal) => Promise<T>) {
    this.#load = load;
    $effect(() => {
      void this.reload();
      return () => this.#controller?.abort();
    });
  }

  async reload(opts: { quiet?: boolean } = {}): Promise<void> {
    this.#controller?.abort();
    const c = new AbortController();
    this.#controller = c;
    // Callers are often effects; reading state here would make them re-run
    // every time the data changes.
    try {
      const value = await untrack(() => {
        if (!opts.quiet) this.loading = this.data === undefined;
        return this.#load(c.signal);
      });
      if (c.signal.aborted) return;
      this.data = value;
      this.error = null;
    } catch (err) {
      if (isAbort(err) || c.signal.aborted) return;
      this.error = errorMessage(err);
      this.status = err instanceof ApiError ? err.status : 0;
    } finally {
      if (!c.signal.aborted) this.loading = false;
    }
  }

  set(value: T): void {
    this.data = value;
  }
}

/** Returns an AbortSignal that fires when the calling component unmounts. */
export function lifetime(): AbortSignal {
  const c = new AbortController();
  $effect(() => () => c.abort());
  return c.signal;
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T;
}

/**
 * Editable copy of a saved value. `dirty` is true while the copy differs,
 * and leaving the page asks for confirmation.
 */
export class Form<T extends object> {
  saved = $state<T>() as T;
  values = $state<T>() as T;
  saving = $state(false);
  dirty = $derived(JSON.stringify(this.values) !== JSON.stringify(this.saved));

  constructor(initial: T) {
    this.saved = clone(initial);
    this.values = clone(initial);
    $effect(() => router.guard(() => this.dirty && !this.saving));
  }

  reset(value?: T): void {
    if (value !== undefined) this.saved = clone(value);
    this.values = clone(this.saved);
  }

  /** Marks the current values as saved (or adopts the server's copy). */
  commit(value?: T): void {
    this.saved = clone(value ?? this.values);
    this.values = clone(this.saved);
  }
}
