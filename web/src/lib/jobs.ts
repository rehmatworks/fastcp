// One poller for every long-running operation: agent jobs (PHP installs,
// rclone, firewall, updates) and backup jobs (backup, restore, export).
// Pass an AbortSignal tied to the page so polling stops on navigation.

import { ApiError, isAbort } from './api';

const DONE = new Set(['succeeded', 'success', 'failed']);

export function isFinished(status: string): boolean {
  return DONE.has(status);
}

export function isFailed(status: string): boolean {
  return status === 'failed';
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(new DOMException('Aborted', 'AbortError'));
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(t);
      reject(new DOMException('Aborted', 'AbortError'));
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

export interface PollOptions<T> {
  signal?: AbortSignal;
  onUpdate?: (job: T) => void;
  /** First delay between polls in milliseconds. */
  interval?: number;
  /** Consecutive network failures tolerated before giving up. */
  maxErrors?: number;
}

/** Polls until the job reaches a terminal status and returns it. */
export async function pollJob<T extends { status: string }>(
  fetchJob: (signal?: AbortSignal) => Promise<T>,
  opts: PollOptions<T> = {},
): Promise<T> {
  let delay = opts.interval ?? 1200;
  let errors = 0;
  const maxErrors = opts.maxErrors ?? 10;
  for (;;) {
    try {
      const job = await fetchJob(opts.signal);
      errors = 0;
      opts.onUpdate?.(job);
      if (isFinished(job.status)) return job;
    } catch (err) {
      if (isAbort(err)) throw err;
      if (err instanceof ApiError && err.status >= 400 && err.status < 500 && err.status !== 429) throw err;
      if (++errors > maxErrors) throw err;
    }
    await sleep(delay, opts.signal);
    delay = Math.min(Math.round(delay * 1.3), 5000);
  }
}

/** Waits until check returns true, polling with backoff. */
export async function waitUntil(
  check: (signal?: AbortSignal) => Promise<boolean>,
  opts: { signal?: AbortSignal; timeout?: number; interval?: number } = {},
): Promise<boolean> {
  const deadline = Date.now() + (opts.timeout ?? 5 * 60_000);
  let delay = opts.interval ?? 2000;
  while (Date.now() < deadline) {
    try {
      if (await check(opts.signal)) return true;
    } catch (err) {
      if (isAbort(err)) throw err;
    }
    await sleep(delay, opts.signal);
    delay = Math.min(delay + 500, 5000);
  }
  return false;
}

export { sleep };
