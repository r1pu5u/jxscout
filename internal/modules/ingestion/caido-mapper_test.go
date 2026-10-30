package ingestion

import (
	"reflect"
	"testing"
)

func TestParseRequestAcceptsMinimalMessageAndCanonicalizesHeaders(t *testing.T) {
	request, err := parseRequest(
		"GET /asset.js HTTP/1.1\r\nX-Trace: first\r\nHost: target.example:8443\r\ncontent-type: application/javascript\r\nReferer: https://target.example/",
		"https://target.example/asset.js",
	)
	if err != nil {
		t.Fatalf("parseRequest() error = %v", err)
	}
	if request.Method != "GET" {
		t.Fatalf("method = %q, want GET", request.Method)
	}
	if request.URL != "https://target.example:8443/asset.js" {
		t.Fatalf("URL = %q, want recovered port", request.URL)
	}
	wantHeaders := map[string]string{
		"X-Trace":      "first",
		"Host":         "target.example:8443",
		"Content-Type": "application/javascript",
		"Referer":      "https://target.example/",
	}
	if !reflect.DeepEqual(request.Headers, wantHeaders) {
		t.Fatalf("headers = %#v, want %#v", request.Headers, wantHeaders)
	}
}

func TestParseRequestAcceptsOnlyRequestLineAndHost(t *testing.T) {
	request, err := parseRequest(
		"GET /asset.js HTTP/1.1\r\nHost: target.example:8080",
		"http://target.example/asset.js",
	)
	if err != nil {
		t.Fatalf("parseRequest() error = %v", err)
	}
	if request.URL != "http://target.example:8080/asset.js" {
		t.Fatalf("URL = %q, want recovered port", request.URL)
	}
}

func TestParseRequestHostPortRecoveryConditions(t *testing.T) {
	tests := []struct {
		name string
		url  string
		host string
		want string
	}{
		{
			name: "adds raw Host port when hostname matches",
			url:  "http://example.test/path.js",
			host: "example.test:8081",
			want: "http://example.test:8081/path.js",
		},
		{
			name: "preserves explicit URL port",
			url:  "http://example.test:9000/path.js",
			host: "example.test:8081",
			want: "http://example.test:9000/path.js",
		},
		{
			name: "does not replace a mismatched hostname",
			url:  "http://example.test/path.js",
			host: "other.test:8081",
			want: "http://example.test/path.js",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := recoverPortFromHost(tt.url, tt.host)
			if err != nil {
				t.Fatalf("recoverPortFromHost() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("recoverPortFromHost() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResponseHTTP2SingleHeaderAndExactBody(t *testing.T) {
	body := "  leading whitespace\r\n\r\ntrailing whitespace \t\r\n"
	response, err := parseResponse("HTTP/2 200\r\ncontent-type: application/javascript\r\n\r\n" + body)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Status != 200 {
		t.Fatalf("status = %d, want 200", response.Status)
	}
	if response.Headers["Content-Type"] != "application/javascript" {
		t.Fatalf("content-type = %q", response.Headers["Content-Type"])
	}
	if response.Body != body {
		t.Fatalf("body bytes changed: got %q, want %q", response.Body, body)
	}
}

func TestSplitHeaderFromBodyPreservesBodyBytes(t *testing.T) {
	body := " \r\nbody\r\n\t"
	got, err := splitHeaderFromBody("GET / HTTP/1.1\r\nHost: example.test\r\n\r\n" + body)
	if err != nil {
		t.Fatalf("splitHeaderFromBody() error = %v", err)
	}
	if got.Body != body {
		t.Fatalf("body bytes changed: got %q, want %q", got.Body, body)
	}
}

func TestParseResponseWithoutHeaders(t *testing.T) {
	response, err := parseResponse("HTTP/1.1 200 OK\r\n\r\nconst value = true;\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != 200 || response.Body != "const value = true;\r\n" {
		t.Fatalf("unexpected response: %#v", response)
	}
}
