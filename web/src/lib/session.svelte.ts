import { api, getToken, setToken, setUnauthorizedHandler, ApiError } from './api';
import { router } from './router.svelte';
import { toast } from './toast.svelte';
import type { LoginResponse, Me, Meta } from './types';

class Session {
  me = $state<Me | null>(null);
  meta = $state<Meta | null>(null);
  ready = $state(false);
  #refreshing: Promise<void> | null = null;
  #timer: ReturnType<typeof setInterval> | null = null;

  /** The account whose resources the UI shows (the impersonated user, if any). */
  get user() {
    return this.me?.user ?? null;
  }

  /** True for administrators who are not acting as another user. */
  get isAdmin(): boolean {
    return !!this.me && this.me.actor.is_admin && !this.me.impersonating;
  }

  async init(): Promise<void> {
    setUnauthorizedHandler(() => this.expire());
    if (getToken()) {
      try {
        await this.load();
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 401)) {
          toast.error(err instanceof Error ? err.message : String(err));
        }
      }
    }
    this.ready = true;
    this.#watch();
  }

  async load(): Promise<void> {
    const [me, meta] = await Promise.all([api.get<Me>('/auth/me'), api.get<Meta>('/meta')]);
    this.me = me;
    this.meta = meta;
  }

  async refreshMeta(): Promise<void> {
    try {
      this.meta = await api.get<Meta>('/meta');
    } catch {
      // keep the previous value
    }
  }

  async login(username: string, password: string): Promise<void> {
    const res = await api.post<LoginResponse>('/auth/login', { username, password }, { anonymous: true });
    setToken(res.token);
    this.me = res.me;
    this.meta = await api.get<Meta>('/meta');
  }

  async logout(): Promise<void> {
    try {
      await api.post('/auth/logout');
    } catch {
      // the token is discarded either way
    }
    this.#clear();
    router.navigate('/login', { force: true, replace: true });
  }

  /** Called when the server rejects the token. */
  expire(): void {
    if (!this.me && !getToken()) return;
    const wasSignedIn = !!this.me;
    this.#clear();
    if (wasSignedIn) toast.info('Your session has ended. Sign in again to continue.');
    const here = router.path + router.search;
    const next = here.startsWith('/login') ? '' : `?next=${encodeURIComponent(here)}`;
    router.navigate(`/login${next}`, { force: true, replace: true });
  }

  setMe(me: Me): void {
    const wasImpersonating = this.me?.impersonating;
    this.me = me;
    if (wasImpersonating !== me.impersonating) void this.refreshMeta();
  }

  async startImpersonation(username: string, password: string, reason: string): Promise<void> {
    const me = await api.post<Me>('/auth/impersonate', { username, password, reason });
    this.setMe(me);
    router.navigate('/', { force: true });
    toast.success(`You are now acting as ${username}.`);
  }

  async stopImpersonation(): Promise<void> {
    const me = await api.del<Me>('/auth/impersonate');
    this.setMe(me);
    router.navigate('/users', { force: true });
  }

  #clear(): void {
    setToken(null);
    this.me = null;
    this.meta = null;
  }

  /**
   * Re-reads the session every minute and when the tab regains focus, so an
   * impersonation that expired on the server is reflected in the UI.
   */
  #watch(): void {
    const refresh = () => {
      if (!this.me || this.#refreshing) return;
      this.#refreshing = api
        .get<Me>('/auth/me')
        .then((me) => {
          if (this.me?.impersonating && !me.impersonating) {
            toast.info('Your time acting as another user has ended.');
            this.setMe(me);
            router.navigate('/', { force: true });
          } else {
            this.setMe(me);
          }
        })
        .catch(() => {})
        .finally(() => {
          this.#refreshing = null;
        });
    };
    this.#timer ??= setInterval(refresh, 60_000);
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') refresh();
    });
  }
}

export const session = new Session();
