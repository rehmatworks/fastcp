// A small typed client for the FastCP API. Every request carries the bearer
// token, honours an AbortSignal, and turns any failure into an ApiError
// with a readable message, whether or not the response body is JSON.

const TOKEN_KEY = 'fastcp.token';

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError';
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError || err instanceof Error) return err.message;
  return String(err);
}

let token: string | null = readToken();
let onUnauthorized: (() => void) | null = null;

function readToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function getToken(): string | null {
  return token;
}

export function setToken(value: string | null): void {
  token = value;
  try {
    if (value) localStorage.setItem(TOKEN_KEY, value);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    // Storage can be unavailable in private modes; the session then lasts
    // until the tab is closed.
  }
}

/** Registers the handler called when an authenticated request gets 401. */
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

const statusText: Record<number, string> = {
  400: 'The request was not valid.',
  401: 'Your session has ended. Sign in again.',
  403: 'You do not have permission to do that.',
  404: 'Not found.',
  409: 'That conflicts with the current state. Refresh and try again.',
  413: 'The request is too large.',
  429: 'Too many attempts. Wait a moment and try again.',
  500: 'The server hit an unexpected error.',
  502: 'The server agent reported an error.',
  503: 'The server agent is unavailable. Try again shortly.',
  504: 'The operation timed out.',
};

async function readError(res: Response): Promise<string> {
  const fallback = statusText[res.status] ?? `Request failed (${res.status}).`;
  let text = '';
  try {
    text = await res.text();
  } catch {
    return fallback;
  }
  if (!text) return fallback;
  try {
    const body = JSON.parse(text) as { error?: unknown; message?: unknown };
    const msg = body.error ?? body.message;
    if (typeof msg === 'string' && msg.trim()) return msg;
  } catch {
    // Not JSON: a proxy error page or plain text.
  }
  const plain = text.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim();
  return plain && plain.length <= 200 ? plain : fallback;
}

export interface RequestOptions {
  signal?: AbortSignal;
  /** Skip the global 401 handler (used by the login form). */
  anonymous?: boolean;
}

export async function request<T>(method: string, path: string, body?: unknown, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (token && !opts.anonymous) headers.Authorization = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(`/api${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: opts.signal,
      credentials: 'omit',
      cache: 'no-store',
    });
  } catch (err) {
    if (isAbort(err)) throw err;
    throw new ApiError(0, 'Could not reach the server. Check your connection.');
  }

  if (res.status === 401 && !opts.anonymous) {
    onUnauthorized?.();
  }
  if (!res.ok) {
    throw new ApiError(res.status, await readError(res));
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (!text) return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new ApiError(res.status, 'The server sent a response that could not be read.');
  }
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, undefined, opts),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>('POST', path, body ?? {}, opts),
  put: <T>(path: string, body: unknown, opts?: RequestOptions) => request<T>('PUT', path, body, opts),
  patch: <T>(path: string, body: unknown, opts?: RequestOptions) => request<T>('PATCH', path, body, opts),
  del: <T = void>(path: string, opts?: RequestOptions) => request<T>('DELETE', path, undefined, opts),
};

/** Encodes one path segment. */
export function seg(value: string | number): string {
  return encodeURIComponent(String(value));
}
