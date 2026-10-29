const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export function bytes(n: number | undefined | null): string {
  if (n === undefined || n === null || !Number.isFinite(n) || n < 0) return '—';
  if (n < 1024) return `${n} B`;
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function megabytes(mb: number): string {
  if (mb < 0) return 'Unlimited';
  if (mb >= 1024 && mb % 1024 === 0) return `${mb / 1024} GB`;
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb} MB`;
}

export function limit(n: number, unit = ''): string {
  if (n < 0) return 'Unlimited';
  return unit ? `${n}${unit}` : String(n);
}

export function percent(used: number, total: number): number {
  if (!total || total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((used / total) * 100)));
}

function toDate(v: string | number | Date | undefined | null): Date | null {
  if (v === undefined || v === null || v === '') return null;
  const d = v instanceof Date ? v : new Date(v);
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000 ? null : d;
}

export function dateTime(v: string | undefined | null): string {
  const d = toDate(v);
  if (!d) return '—';
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

export function date(v: string | undefined | null): string {
  const d = toDate(v);
  if (!d) return '—';
  return d.toLocaleDateString(undefined, { dateStyle: 'medium' });
}

const rtf = typeof Intl !== 'undefined' ? new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }) : null;

export function relative(v: string | undefined | null, now = Date.now()): string {
  const d = toDate(v);
  if (!d || !rtf) return '—';
  const s = Math.round((d.getTime() - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return s <= 0 ? 'just now' : 'in a few seconds';
  if (abs < 3600) return rtf.format(Math.round(s / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(s / 3600), 'hour');
  if (abs < 86400 * 30) return rtf.format(Math.round(s / 86400), 'day');
  if (abs < 86400 * 365) return rtf.format(Math.round(s / (86400 * 30)), 'month');
  return rtf.format(Math.round(s / (86400 * 365)), 'year');
}

export function duration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  const s = Math.round(seconds);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ${m % 60}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

export function elapsed(start: string | undefined, end?: string): string {
  const a = toDate(start);
  if (!a) return '—';
  const b = toDate(end) ?? new Date();
  return duration((b.getTime() - a.getTime()) / 1000);
}

export function siteType(type: string): string {
  if (type === 'wordpress') return 'WordPress';
  if (type === 'proxy') return 'Reverse proxy';
  return 'PHP app';
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function pad2(n: number): string {
  return String(n).padStart(2, '0');
}
