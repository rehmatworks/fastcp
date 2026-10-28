import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, setToken, setUnauthorizedHandler } from './api';

function respond(status: number, body: string, contentType = 'application/json') {
  return vi.fn(async () => new Response(status === 204 ? null : body, { status, headers: { 'Content-Type': contentType } }));
}

async function failure(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p;
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError);
    return err as ApiError;
  }
  throw new Error('expected the request to fail');
}

afterEach(() => {
  vi.unstubAllGlobals();
  setToken(null);
  setUnauthorizedHandler(() => {});
});

describe('request', () => {
  it('sends JSON with the bearer token and parses the response', async () => {
    const fetch = respond(200, '{"ok":true}');
    vi.stubGlobal('fetch', fetch);
    setToken('secret');
    expect(await api.post('/things', { a: 1 })).toEqual({ ok: true });
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/things');
    expect(init.method).toBe('POST');
    expect(init.body).toBe('{"a":1}');
    expect(init.credentials).toBe('omit');
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer secret');
  });

  it('omits the token for anonymous requests', async () => {
    const fetch = respond(200, '{}');
    vi.stubGlobal('fetch', fetch);
    setToken('secret');
    await api.get('/version', { anonymous: true });
    const init = (fetch.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it('returns undefined for empty responses', async () => {
    vi.stubGlobal('fetch', respond(204, ''));
    expect(await api.del('/things/1')).toBeUndefined();
  });
});

describe('errors', () => {
  it('uses the JSON error message', async () => {
    vi.stubGlobal('fetch', respond(400, '{"error":"The domain is taken."}'));
    const err = await failure(api.get('/x'));
    expect(err.status).toBe(400);
    expect(err.message).toBe('The domain is taken.');
  });

  it('uses short plain-text bodies', async () => {
    vi.stubGlobal('fetch', respond(502, 'Bad gateway from proxy', 'text/plain'));
    expect((await failure(api.get('/x'))).message).toBe('Bad gateway from proxy');
  });

  it('strips HTML and falls back for long pages', async () => {
    vi.stubGlobal('fetch', respond(503, `<html><body>${'x'.repeat(500)}</body></html>`, 'text/html'));
    expect((await failure(api.get('/x'))).message).toBe('The server agent is unavailable. Try again shortly.');
  });

  it('falls back to a status message for empty bodies', async () => {
    vi.stubGlobal('fetch', respond(418, ''));
    expect((await failure(api.get('/x'))).message).toBe('Request failed (418).');
  });

  it('reports network failures with status 0', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch');
      }),
    );
    const err = await failure(api.get('/x'));
    expect(err.status).toBe(0);
  });

  it('rethrows aborts unchanged', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new DOMException('Aborted', 'AbortError');
      }),
    );
    await expect(api.get('/x')).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('calls the unauthorized handler on 401 unless anonymous', async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    vi.stubGlobal('fetch', respond(401, '{"error":"no"}'));
    await failure(api.get('/x'));
    expect(handler).toHaveBeenCalledTimes(1);
    await failure(api.post('/auth/login', {}, { anonymous: true }));
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it('rejects unreadable success bodies', async () => {
    vi.stubGlobal('fetch', respond(200, '{not json'));
    expect((await failure(api.get('/x'))).message).toMatch(/could not be read/);
  });
});
