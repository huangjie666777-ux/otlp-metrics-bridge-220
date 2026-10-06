package main

import (
	"errors"
	"io"
	"net/http"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func NewHandler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/metrics", func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != "application/x-protobuf" && request.Header.Get("Content-Type") != "application/protobuf" {
			http.Error(writer, "unsupported content type", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var metricsRequest collectorpb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &metricsRequest); err != nil {
			http.Error(writer, "invalid protobuf: "+err.Error(), http.StatusBadRequest)
			return
		}
		pending, err := parseRequest(store, &metricsRequest)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.Commit(pending); err != nil {
			var invalid invalidInputError
			if errors.As(err, &invalid) {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		response, err := proto.Marshal(&collectorpb.ExportMetricsServiceResponse{})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = writer.Write(response)
	})
	mux.HandleFunc("GET /metrics", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(writer, RenderPrometheus(store.Snapshot()))
	})
	return mux
}
