package main

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Addr           string
	MaxSeries      int
	IdleExpireSecs int64
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Addr:           getenv("OTLP_HTTP_ADDR", ":4318"),
		MaxSeries:      10000,
		IdleExpireSecs: 300,
	}
	if port := os.Getenv("PORT"); port != "" {
		cfg.Addr = ":" + port
	}
	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if value := os.Getenv("OTLP_MAX_SERIES"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("OTLP_MAX_SERIES must be a positive integer")
		}
		cfg.MaxSeries = parsed
	}
	if value := os.Getenv("OTLP_IDLE_EXPIRE_SECONDS"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return Config{}, fmt.Errorf("OTLP_IDLE_EXPIRE_SECONDS must be a non-negative integer")
		}
		cfg.IdleExpireSecs = parsed
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
