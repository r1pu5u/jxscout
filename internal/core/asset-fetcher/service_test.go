package assetfetcher

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRedirectOriginAndScope(t *testing.T) {
	fetcher := NewAssetFetcher(AssetFetcherOptions{AllowedURL: func(raw string) bool { return !strings.Contains(raw, "denied") }})
	original, _ := http.NewRequest("GET", "https://app.test/entry.js", nil)
	other, _ := http.NewRequest("GET", "https://cdn.test/chunk.js", nil)
	other.Header.Set("X-Api-Key", "secret")
	if err := fetcher.client.CheckRedirect(other, []*http.Request{original}); err != nil {
		t.Fatal(err)
	}
	if other.Header.Get("X-Api-Key") != "" {
		t.Fatal("cross-origin redirect retained custom credentials")
	}
	denied, _ := http.NewRequest("GET", "https://denied.test/chunk.js", nil)
	if err := fetcher.client.CheckRedirect(denied, []*http.Request{original}); err == nil {
		t.Fatal("accepted excluded redirect")
	}
	same, _ := http.NewRequest("GET", "https://app.test/chunk.js", nil)
	same.Header.Set("Authorization", "Bearer secret")
	if err := fetcher.client.CheckRedirect(same, []*http.Request{original}); err != nil {
		t.Fatal(err)
	}
	if same.Header.Get("Authorization") == "" {
		t.Fatal("lost same-origin credentials")
	}
}

func gzipPayload(t testing.TB, body string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestReadResponseStreamsGzipAndRejectsFailures(t *testing.T) {
	source := strings.Repeat("const chunk = true;\n", 100000)
	compressed := gzipPayload(t, source)
	for _, header := range []string{"gzip", ""} {
		response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{header}}, Body: io.NopCloser(bytes.NewReader(compressed))}
		body, found, err := readResponse(response)
		if err != nil || !found || body != source {
			t.Fatalf("gzip header=%q bodylen=%d found=%v err=%v", header, len(body), found, err)
		}
	}
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(source))}
	if body, found, err := readResponse(response); err != nil || !found || body != source {
		t.Fatal("plain script changed")
	}
	bad := &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(strings.NewReader("broken gzip"))}
	if _, _, err := readResponse(bad); err == nil {
		t.Fatal("invalid gzip accepted")
	}
	failedReader := &countingReader{}
	failed := &http.Response{StatusCode: 404, ContentLength: 20 << 20, Header: make(http.Header), Body: io.NopCloser(failedReader)}
	if body, found, err := readResponse(failed); err != nil || found || body != "" || failedReader.reads != 0 {
		t.Fatal("large failed response body was read")
	}
}

type countingReader struct{ reads int }

func (r *countingReader) Read(p []byte) (int, error) { r.reads++; return 0, io.EOF }

var responseBenchmarkSink string

func BenchmarkGzipResponseMemory(b *testing.B) {
	defer func() { responseBenchmarkSink = "" }()
	source := strings.Repeat("const chunk = true;\n", 300000)
	compressed := gzipPayload(b, source)
	b.Run("previous", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(source)))
		for i := 0; i < b.N; i++ {
			wire, err := io.ReadAll(bytes.NewReader(compressed))
			if err != nil {
				b.Fatal(err)
			}
			reader, err := gzip.NewReader(bytes.NewReader(wire))
			if err != nil {
				b.Fatal(err)
			}
			decoded, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				b.Fatal(err)
			}
			text := string(decoded)
			responseBenchmarkSink = text
			if len(text) != len(source) {
				b.Fatal("response truncated")
			}
		}
	})
	b.Run("streaming", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(source)))
		for i := 0; i < b.N; i++ {
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(compressed))}
			text, found, err := readResponse(response)
			responseBenchmarkSink = text
			if err != nil || !found || len(text) != len(source) {
				b.Fatal("response truncated", err)
			}
		}
	})
}
