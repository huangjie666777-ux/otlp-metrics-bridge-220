package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestHTTPExportAndScrape(t *testing.T) {
	store := NewStore(10, 0, func() time.Time { return time.Unix(100, 0) })
	server := httptest.NewServer(NewHandler(store))
	defer server.Close()

	payload, err := proto.Marshal(counterRequest("requests", 100, 200, 5))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/metrics", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var exported collectorpb.ExportMetricsServiceResponse
	if err := proto.Unmarshal(readAll(t, response), &exported); err != nil {
		t.Fatal(err)
	}

	scrape, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer scrape.Body.Close()
	body := string(readAll(t, scrape))
	if !strings.Contains(body, `requests_total{instance="host",scope="svc"} 5`) {
		t.Fatalf("unexpected metrics output: %s", body)
	}
}

func TestHTTPRejectsUnsupportedContentType(t *testing.T) {
	store := NewStore(10, 0, func() time.Time { return time.Unix(100, 0) })
	server := httptest.NewServer(NewHandler(store))
	defer server.Close()

	response, err := http.Post(server.URL+"/v1/metrics", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusUnsupportedMediaType)
	}
}

func readAll(t *testing.T, response *http.Response) []byte {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
