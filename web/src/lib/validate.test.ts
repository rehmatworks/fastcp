import { describe, expect, it } from 'vitest';
import { domainError, parseSize, passwordError, usernameError } from './validate';

describe('parseSize', () => {
  it('parses PHP shorthand sizes', () => {
    expect(parseSize('256M')).toBe(256 * 1024 * 1024);
    expect(parseSize('1g')).toBe(1024 ** 3);
    expect(parseSize(' 64 K ')).toBe(64 * 1024);
    expect(parseSize('1024')).toBe(1024);
  });

  it('rejects anything else', () => {
    expect(parseSize('')).toBeNull();
    expect(parseSize('-1')).toBeNull();
    expect(parseSize('1.5G')).toBeNull();
    expect(parseSize('12T')).toBeNull();
    expect(parseSize('256M\nfoo')).toBeNull();
  });
});

describe('domainError', () => {
  it('accepts normal domains', () => {
    expect(domainError('example.com')).toBeNull();
    expect(domainError('Sub.Example.co.uk.')).toBeNull();
    expect(domainError('xn--bcher-kva.example')).toBeNull();
  });

  it('rejects malformed input', () => {
    expect(domainError('')).not.toBeNull();
    expect(domainError('localhost')).not.toBeNull();
    expect(domainError('192.168.1.1')).not.toBeNull();
    expect(domainError('[::1]')).not.toBeNull();
    expect(domainError('-bad.example.com')).not.toBeNull();
    expect(domainError('bad-.example.com')).not.toBeNull();
    expect(domainError('under_score.example.com')).not.toBeNull();
    expect(domainError(`${'a'.repeat(64)}.com`)).not.toBeNull();
  });
});

describe('usernameError', () => {
  it('matches the server rules', () => {
    expect(usernameError('alice')).toBeNull();
    expect(usernameError('a1')).toBeNull();
    expect(usernameError('web_user-2')).toBeNull();
    expect(usernameError('a'.repeat(32))).toBeNull();
  });

  it('rejects invalid usernames', () => {
    expect(usernameError('')).not.toBeNull();
    expect(usernameError('a')).not.toBeNull();
    expect(usernameError('1alice')).not.toBeNull();
    expect(usernameError('Alice')).not.toBeNull();
    expect(usernameError('a'.repeat(33))).not.toBeNull();
    expect(usernameError('al ice')).not.toBeNull();
  });
});

describe('passwordError', () => {
  it('enforces 8 to 128 characters', () => {
    expect(passwordError('short')).not.toBeNull();
    expect(passwordError('12345678')).toBeNull();
    expect(passwordError('x'.repeat(128))).toBeNull();
    expect(passwordError('x'.repeat(129))).not.toBeNull();
  });
});
