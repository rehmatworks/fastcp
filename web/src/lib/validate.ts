// Client-side checks that mirror the server's rules, for instant feedback.
// The server always validates again.

/** Parses a PHP size such as 256M or 1G into bytes, or null if invalid. */
export function parseSize(input: string): number | null {
  const m = /^\s*(\d{1,6})\s*([KMG]?)\s*$/i.exec(input);
  if (!m) return null;
  const n = Number(m[1]);
  const mult = { '': 1, K: 1 << 10, M: 1 << 20, G: 1 << 30 }[m[2].toUpperCase() as '' | 'K' | 'M' | 'G'];
  return n * mult;
}

const label = /^(?!-)[a-z0-9-]{1,63}(?<!-)$/;

/** Returns an error message for an obviously invalid domain, or null. */
export function domainError(input: string): string | null {
  const d = input.trim().toLowerCase().replace(/\.$/, '');
  if (!d) return 'Enter a domain.';
  if (d.length > 253) return 'The domain is too long.';
  if (/^\d+(\.\d+){3}$/.test(d) || d.includes(':')) return 'Use a domain name, not an IP address.';
  const parts = d.split('.');
  if (parts.length < 2) return 'Enter a full domain such as example.com.';
  if (!parts.every((p) => label.test(p))) return 'Domains may only contain letters, digits, hyphens and dots.';
  return null;
}

export const usernamePattern = /^[a-z][a-z0-9_-]{1,31}$/;

export function usernameError(input: string): string | null {
  if (!input) return 'Enter a username.';
  if (!usernamePattern.test(input)) return '2–32 characters: lowercase letters, digits, - and _, starting with a letter.';
  return null;
}

export function passwordError(input: string): string | null {
  if (input.length < 8) return 'Use at least 8 characters.';
  if (input.length > 128) return 'Use at most 128 characters.';
  return null;
}

/** Five-field cron presets offered in schedule pickers. */
export const cronPresets = [
  { value: '* * * * *', label: 'Every minute' },
  { value: '*/5 * * * *', label: 'Every 5 minutes' },
  { value: '*/15 * * * *', label: 'Every 15 minutes' },
  { value: '0 * * * *', label: 'Every hour' },
  { value: '0 */6 * * *', label: 'Every 6 hours' },
  { value: '0 0 * * *', label: 'Every day at midnight' },
  { value: '0 3 * * *', label: 'Every day at 03:00' },
  { value: '0 0 * * 0', label: 'Every Sunday at midnight' },
  { value: '0 0 1 * *', label: 'On the 1st of every month' },
];
