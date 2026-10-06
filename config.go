package main

import (
	"os"
	"strconv"
)

type Config struct {
	Port          int
	MaxSeries     int
	ExpirySeconds int
}

func LoadConfig() Config {
	return Config{
		Port:          envInt("OTLP220_PORT", 8080),
		MaxSeries:     envInt("OTLP220_MAX_SERIES", 10000),
		ExpirySeconds: envInt("OTLP220_EXPIRY_SECONDS", 300),
	}
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
