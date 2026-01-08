package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	addr := flag.String("addr", "https://localhost:8080", "FastCP base URL")
	token := flag.String("token", "", "Bearer token for admin auth (optional)")
	insecure := flag.Bool("insecure", true, "skip TLS verification")
	flag.Parse()

	client := &http.Client{}
	if *insecure {
		tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		client.Transport = tr
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/users/fix-permissions", *addr), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create request:", err)
		os.Exit(1)
	}
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "request failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
}
