// Package main: one small HTTP client for the mapping endpoints.
package main

import (
	"bytes"
	"io"
	"net/http"
	"time"
)

const (
	headerToken = "X-Hark-Fs-Token"
	headerType  = "Content-Type"
	callTimeout = 180 * time.Second
)

type reply struct {
	status int
	body   []byte
}

func call(doc *config, method, path string, body []byte, contentType string) (*reply, error) {
	return callWith(doc, doc.Token, method, path, body, contentType)
}

func callWith(doc *config, token, method, path string, body []byte, contentType string) (*reply, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, doc.Base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set(headerToken, token)
	ua := doc.UA
	if ua == "" {
		ua = appName + "/1"
	}
	req.Header.Set("User-Agent", ua)
	if contentType != "" {
		req.Header.Set(headerType, contentType)
	}
	client := &http.Client{Timeout: callTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &reply{status: resp.StatusCode, body: raw}, nil
}
