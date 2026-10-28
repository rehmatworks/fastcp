import { describe, expect, it } from 'vitest';
import { isActive, matchPath } from './router.svelte';

describe('matchPath', () => {
  it('matches static paths', () => {
    expect(matchPath('/', '/')).toEqual({});
    expect(matchPath('/sites', '/sites')).toEqual({});
    expect(matchPath('/sites', '/sites/')).toEqual({});
    expect(matchPath('/sites', '/databases')).toBeNull();
  });

  it('extracts and decodes parameters', () => {
    expect(matchPath('/sites/:id', '/sites/abc')).toEqual({ id: 'abc' });
    expect(matchPath('/users/:name', '/users/a%20b')).toEqual({ name: 'a b' });
  });

  it('supports optional trailing parameters', () => {
    expect(matchPath('/sites/:id/:tab?', '/sites/abc')).toEqual({ id: 'abc' });
    expect(matchPath('/sites/:id/:tab?', '/sites/abc/php')).toEqual({ id: 'abc', tab: 'php' });
    expect(matchPath('/settings/:section?', '/settings')).toEqual({});
  });

  it('rejects extra segments and missing required ones', () => {
    expect(matchPath('/sites/:id/:tab?', '/sites/abc/php/extra')).toBeNull();
    expect(matchPath('/sites/:id', '/sites')).toBeNull();
  });

  it('rejects malformed escapes', () => {
    expect(matchPath('/sites/:id', '/sites/%E0%A4%A')).toBeNull();
  });
});

describe('isActive', () => {
  it('only matches the root exactly', () => {
    expect(isActive('/', '/')).toBe(true);
    expect(isActive('/', '/sites')).toBe(false);
  });

  it('matches a section and its children but not siblings with a shared prefix', () => {
    expect(isActive('/sites', '/sites')).toBe(true);
    expect(isActive('/sites', '/sites/abc/php')).toBe(true);
    expect(isActive('/sites', '/sitesx')).toBe(false);
  });
});
