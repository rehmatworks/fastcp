import { describe, expect, it } from 'vitest';
import { bytes, dateTime, duration, elapsed, limit, megabytes, pad2, percent, plural, relative } from './format';

describe('bytes', () => {
  it('formats sizes with binary units', () => {
    expect(bytes(0)).toBe('0 B');
    expect(bytes(1023)).toBe('1023 B');
    expect(bytes(1024)).toBe('1.0 KB');
    expect(bytes(1536)).toBe('1.5 KB');
    expect(bytes(150 * 1024 * 1024)).toBe('150 MB');
    expect(bytes(5 * 1024 ** 4)).toBe('5.0 TB');
  });

  it('shows a dash for missing or invalid values', () => {
    expect(bytes(undefined)).toBe('—');
    expect(bytes(null)).toBe('—');
    expect(bytes(-1)).toBe('—');
    expect(bytes(Number.NaN)).toBe('—');
  });
});

describe('limits', () => {
  it('treats negative values as unlimited', () => {
    expect(megabytes(-1)).toBe('Unlimited');
    expect(limit(-1, '%')).toBe('Unlimited');
  });

  it('formats megabytes', () => {
    expect(megabytes(512)).toBe('512 MB');
    expect(megabytes(2048)).toBe('2 GB');
    expect(megabytes(1536)).toBe('1.5 GB');
  });

  it('formats plain limits', () => {
    expect(limit(100, '%')).toBe('100%');
    expect(limit(3)).toBe('3');
  });
});

describe('percent', () => {
  it('clamps and rounds', () => {
    expect(percent(1, 3)).toBe(33);
    expect(percent(5, 0)).toBe(0);
    expect(percent(10, 5)).toBe(100);
    expect(percent(-1, 5)).toBe(0);
  });
});

describe('time', () => {
  const now = Date.parse('2026-01-15T12:00:00Z');

  it('describes relative times', () => {
    expect(relative('2026-01-15T11:59:50Z', now)).toBe('just now');
    expect(relative('2026-01-15T11:55:00Z', now)).toBe('5 minutes ago');
    expect(relative('2026-01-15T15:00:00Z', now)).toBe('in 3 hours');
    expect(relative('2026-01-13T12:00:00Z', now)).toBe('2 days ago');
  });

  it('ignores zero and invalid dates', () => {
    expect(relative('0001-01-01T00:00:00Z', now)).toBe('—');
    expect(relative('', now)).toBe('—');
    expect(relative(undefined, now)).toBe('—');
    expect(dateTime('not a date')).toBe('—');
  });

  it('formats durations', () => {
    expect(duration(5)).toBe('5s');
    expect(duration(65)).toBe('1m 5s');
    expect(duration(3 * 3600 + 120)).toBe('3h 2m');
    expect(duration(3 * 86400 + 3600)).toBe('3d 1h');
    expect(duration(-1)).toBe('—');
  });

  it('measures elapsed time between two timestamps', () => {
    expect(elapsed('2026-01-15T12:00:00Z', '2026-01-15T12:01:30Z')).toBe('1m 30s');
    expect(elapsed(undefined)).toBe('—');
  });
});

describe('text helpers', () => {
  it('pluralises', () => {
    expect(plural(1, 'site')).toBe('1 site');
    expect(plural(2, 'site')).toBe('2 sites');
    expect(plural(0, 'entry', 'entries')).toBe('0 entries');
  });

  it('pads', () => {
    expect(pad2(3)).toBe('03');
    expect(pad2(12)).toBe('12');
  });
});
