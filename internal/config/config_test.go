package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_PATH", "")
	t.Setenv("PORT", "")

	got := Load()
	if got.DatabasePath != "./data/incidentlens.db" {
		t.Errorf("DatabasePath = %q, want default", got.DatabasePath)
	}
	if got.Port != "8080" {
		t.Errorf("Port = %q, want default 8080", got.Port)
	}
}

func TestLoadEnvironmentOverrides(t *testing.T) {
	t.Setenv("DATABASE_PATH", "/tmp/test-incidents.db")
	t.Setenv("PORT", "9090")

	got := Load()
	if got.DatabasePath != "/tmp/test-incidents.db" {
		t.Errorf("DatabasePath = %q, want environment value", got.DatabasePath)
	}
	if got.Port != "9090" {
		t.Errorf("Port = %q, want environment value", got.Port)
	}
}
