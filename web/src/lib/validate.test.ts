import { describe, expect, it } from 'vitest';
import { domainError, parseSize, passwordError, proxyError, usernameError } from './validate';

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

describe('proxyError', () => {
  it('accepts application addresses the server accepts', () => {
    expect(proxyError('3000')).toBeNull();
    expect(proxyError('127.0.0.1:8080')).toBeNull();
    expect(proxyError('http://localhost:5678/')).toBeNull();
    expect(proxyError('https://backend.example.com')).toBeNull();
    expect(proxyError('http://[::1]:3000')).toBeNull();
  });

  it('rejects anything with more than a host and a port', () => {
    expect(proxyError('')).not.toBeNull();
    expect(proxyError('70000')).not.toBeNull();
    expect(proxyError('ftp://127.0.0.1:21')).not.toBeNull();
    expect(proxyError('http://127.0.0.1:3000/app')).not.toBeNull();
    expect(proxyError('http://user:pw@127.0.0.1:3000')).not.toBeNull();
    expect(proxyError('http://127.0.0.1:3000?')).not.toBeNull();
    expect(proxyError('http://127.0.0.1:99999')).not.toBeNull();
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
