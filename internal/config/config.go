package config

import "os"

type Config struct {
	DatabasePath string
	Port         string
	Namespace    string
}

func envOr(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func Load() *Config {
	cfg := &Config{
		DatabasePath: envOr("DATABASE_PATH", "./data/incidentlens.db"),
		Port:         envOr("PORT", "8080"),
		Namespace:    envOr("NAMESPACE", "incident-lens"),
	}

	return cfg
}
