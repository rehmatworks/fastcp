package api

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"sync"
	"time"
)

const maxTokens = 10000

type tokenEntry[T any] struct {
	value   T
	expires time.Time
}

// tokenStore keeps short-lived random tokens in memory.
type tokenStore[T any] struct {
	mu sync.Mutex
	m  map[string]tokenEntry[T]
}

func newTokenStore[T any]() *tokenStore[T] {
	return &tokenStore[T]{m: map[string]tokenEntry[T]{}}
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (t *tokenStore[T]) put(v T, ttl time.Duration) string {
	token := randomToken()
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.m) >= maxTokens {
		t.gcLocked(now)
	}
	t.m[token] = tokenEntry[T]{value: v, expires: now.Add(ttl)}
	return token
}

// take returns and removes a token's value.
func (t *tokenStore[T]) take(token string) (T, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[token]
	delete(t.m, token)
	if !ok || time.Now().After(e.expires) {
		var zero T
		return zero, false
	}
	return e.value, true
}

// peek returns a token's value without consuming it.
func (t *tokenStore[T]) peek(token string) (T, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[token]
	if !ok || time.Now().After(e.expires) {
		delete(t.m, token)
		var zero T
		return zero, false
	}
	return e.value, true
}

func (t *tokenStore[T]) remove(token string) {
	t.mu.Lock()
	delete(t.m, token)
	t.mu.Unlock()
}

func (t *tokenStore[T]) gc(now time.Time) {
	t.mu.Lock()
	t.gcLocked(now)
	t.mu.Unlock()
}

func (t *tokenStore[T]) gcLocked(now time.Time) {
	for k, e := range t.m {
		if now.After(e.expires) {
			delete(t.m, k)
		}
	}
}

const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// randomPassword returns n characters that are safe in shells, URLs and
// PHP string literals.
func randomPassword(n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		out[i] = passwordAlphabet[v.Int64()]
	}
	return string(out)
}
