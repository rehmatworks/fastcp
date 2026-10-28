import { describe, expect, it, vi } from 'vitest';
import { ApiError } from './api';
import { isFailed, isFinished, pollJob, waitUntil } from './jobs';

type Job = { status: string };

function sequence(...steps: (Job | Error)[]) {
  let i = 0;
  return vi.fn(async (): Promise<Job> => {
    const step = steps[Math.min(i++, steps.length - 1)];
    if (step instanceof Error) throw step;
    return step;
  });
}

describe('job status', () => {
  it('recognises terminal states from both job systems', () => {
    expect(isFinished('succeeded')).toBe(true);
    expect(isFinished('success')).toBe(true);
    expect(isFinished('failed')).toBe(true);
    expect(isFinished('running')).toBe(false);
    expect(isFinished('queued')).toBe(false);
    expect(isFailed('failed')).toBe(true);
    expect(isFailed('success')).toBe(false);
  });
});

describe('pollJob', () => {
  it('polls until the job finishes and reports progress', async () => {
    const fetch = sequence({ status: 'queued' }, { status: 'running' }, { status: 'succeeded' });
    const seen: string[] = [];
    const job = await pollJob(fetch, { interval: 1, onUpdate: (j) => seen.push(j.status) });
    expect(job.status).toBe('succeeded');
    expect(seen).toEqual(['queued', 'running', 'succeeded']);
  });

  it('tolerates transient network errors', async () => {
    const fetch = sequence(new ApiError(0, 'offline'), new ApiError(503, 'restarting'), { status: 'failed' });
    expect((await pollJob(fetch, { interval: 1 })).status).toBe('failed');
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it('gives up after too many consecutive errors', async () => {
    const fetch = sequence(new ApiError(0, 'offline'));
    await expect(pollJob(fetch, { interval: 1, maxErrors: 2 })).rejects.toThrow('offline');
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it('stops immediately on client errors other than 429', async () => {
    const fetch = sequence(new ApiError(404, 'gone'));
    await expect(pollJob(fetch, { interval: 1 })).rejects.toThrow('gone');
    expect(fetch).toHaveBeenCalledTimes(1);

    const limited = sequence(new ApiError(429, 'slow down'), { status: 'success' });
    expect((await pollJob(limited, { interval: 1 })).status).toBe('success');
  });

  it('stops when aborted', async () => {
    const c = new AbortController();
    const fetch = sequence({ status: 'running' });
    const p = pollJob(fetch, { interval: 50, signal: c.signal });
    c.abort();
    await expect(p).rejects.toMatchObject({ name: 'AbortError' });
  });
});

describe('waitUntil', () => {
  it('resolves true once the check passes', async () => {
    let n = 0;
    expect(await waitUntil(async () => ++n >= 3, { interval: 1 })).toBe(true);
    expect(n).toBe(3);
  });

  it('treats errors as not ready and times out', async () => {
    const check = vi.fn(async () => {
      throw new Error('down');
    });
    expect(await waitUntil(check, { interval: 1, timeout: 20 })).toBe(false);
    expect(check).toHaveBeenCalled();
  });
});
