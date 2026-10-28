// History-API router. Same-origin links are intercepted globally, and pages
// with unsaved changes can register a guard that asks before leaving.

import { untrack } from 'svelte';

export type Params = Record<string, string>;

/**
 * Matches a path against a pattern such as "/sites/:id/:tab?". Returns the
 * decoded parameters, or null when the path does not match.
 */
export function matchPath(pattern: string, path: string): Params | null {
  const pp = pattern.split('/').filter(Boolean);
  const sp = path.split('/').filter(Boolean);
  const params: Params = {};
  let i = 0;
  for (; i < pp.length; i++) {
    const part = pp[i];
    const optional = part.endsWith('?');
    const name = optional ? part.slice(0, -1) : part;
    const value = sp[i];
    if (value === undefined) {
      if (optional) continue;
      return null;
    }
    if (name.startsWith(':')) {
      try {
        params[name.slice(1)] = decodeURIComponent(value);
      } catch {
        return null;
      }
    } else if (name !== value) {
      return null;
    }
  }
  return sp.length <= pp.length ? params : null;
}

type Guard = () => boolean;

const LEAVE_PROMPT = 'You have unsaved changes. Leave this page and discard them?';

class Router {
  path = $state(typeof location === 'undefined' ? '/' : location.pathname);
  search = $state(typeof location === 'undefined' ? '' : location.search);
  #guards = new Set<Guard>();
  #started = false;

  start(): void {
    if (this.#started) return;
    this.#started = true;
    window.addEventListener('popstate', () => {
      if (this.#dirty() && !confirm(LEAVE_PROMPT)) {
        history.pushState(null, '', this.path + this.search);
        return;
      }
      this.#sync();
    });
    window.addEventListener('beforeunload', (e) => {
      if (this.#dirty()) e.preventDefault();
    });
    document.addEventListener('click', (e) => this.#onClick(e));
  }

  get query(): URLSearchParams {
    return new URLSearchParams(this.search);
  }

  /** Navigates to a same-origin path. Returns false if a guard cancelled. */
  navigate(to: string, opts: { replace?: boolean; force?: boolean } = {}): boolean {
    const url = new URL(to, location.href);
    if (url.pathname === this.path && url.search === this.search && !url.hash) return true;
    if (!opts.force && this.#dirty() && !confirm(LEAVE_PROMPT)) return false;
    if (opts.replace) history.replaceState(null, '', url.pathname + url.search + url.hash);
    else history.pushState(null, '', url.pathname + url.search + url.hash);
    this.#sync();
    if (!opts.replace) window.scrollTo(0, 0);
    return true;
  }

  /** Updates the query string without a history entry or guard check. */
  setQuery(params: Record<string, string | null>): void {
    untrack(() => {
      const q = this.query;
      for (const [k, v] of Object.entries(params)) {
        if (v === null || v === '') q.delete(k);
        else q.set(k, v);
      }
      const s = q.toString();
      const next = s ? `?${s}` : '';
      if (next === this.search) return;
      history.replaceState(null, '', this.path + next);
      this.#sync();
    });
  }

  /** Registers a guard that returns true while there are unsaved changes. */
  guard(fn: Guard): () => void {
    this.#guards.add(fn);
    return () => this.#guards.delete(fn);
  }

  #dirty(): boolean {
    for (const g of this.#guards) if (g()) return true;
    return false;
  }

  #sync(): void {
    this.path = location.pathname;
    this.search = location.search;
  }

  #onClick(e: MouseEvent): void {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = (e.target as Element | null)?.closest?.('a');
    if (!a || !a.href || a.target || a.hasAttribute('download') || a.getAttribute('rel')?.includes('external')) return;
    const url = new URL(a.href);
    if (url.origin !== location.origin) return;
    if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/phpmyadmin')) return;
    e.preventDefault();
    this.navigate(url.pathname + url.search + url.hash);
  }
}

export const router = new Router();

/** Returns true if path is at or below base, for navigation highlighting. */
export function isActive(base: string, path: string): boolean {
  if (base === '/') return path === '/';
  return path === base || path.startsWith(`${base}/`);
}
