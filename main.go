package main

import (
	"log"
	"net/http"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	store := NewStore(cfg.MaxSeries, cfg.IdleExpireSecs, nil)
	server := &http.Server{Addr: cfg.Addr, Handler: NewHandler(store)}
	log.Printf("OTLP metrics converter listening on %s (max series=%d, idle expiry seconds=%d)", cfg.Addr, cfg.MaxSeries, cfg.IdleExpireSecs)
	log.Fatal(server.ListenAndServe())
}
