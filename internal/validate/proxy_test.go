package validate

import "testing"

func TestProxyURL(t *testing.T) {
	reserved := []int{80, 443, 2050, 2088}
	good := map[string]string{
		"3000":                        "http://127.0.0.1:3000",
		"127.0.0.1:8080":              "http://127.0.0.1:8080",
		"localhost:5678":              "http://localhost:5678",
		" HTTP://LocalHost:5678/ ":    "http://localhost:5678",
		"http://[::1]:3000":           "http://[::1]:3000",
		"http://172.17.0.2:8080":      "http://172.17.0.2:8080",
		"http://app:8000":             "http://app:8000",
		"https://backend.example.com": "https://backend.example.com",
		"http://10.0.0.5":             "http://10.0.0.5",
		"http://[2001:db8::1]":        "http://[2001:db8::1]",
		"http://127.0.0.1:03000":      "http://127.0.0.1:3000",
	}
	for in, want := range good {
		got, err := ProxyURL(in, reserved...)
		if err != nil || got != want {
			t.Errorf("ProxyURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "0", "70000", "ftp://127.0.0.1:21", "unix//run/php.sock", "http://127.0.0.1:3000/app",
		"http://user:pass@127.0.0.1:3000", "http://127.0.0.1:3000?x=1", "http://127.0.0.1:3000#x",
		"http://169.254.169.254", "http://[fe80::1]:80", "http://0.0.0.0:3000", "http://[::]:3000",
		"http://127.0.0.1", "https://localhost", "http://localhost:2050", "https://127.0.0.1:2088",
		"http://[::1]:443", "http://a.localhost:2050", "http://{host}:3000", "http://a b:3000",
		"http://127.0.0.1:3000 {", "http://ex_ample.com", "http://example.com:http",
	}
	for _, in := range bad {
		if got, err := ProxyURL(in, reserved...); err == nil {
			t.Errorf("ProxyURL(%q) = %q, want error", in, got)
		}
	}
	if got, err := ProxyURL("http://127.0.0.1:2050"); err != nil || got != "http://127.0.0.1:2050" {
		t.Errorf("without reserved ports = %q, %v", got, err)
	}
}
