package config

import (
	"testing"
)

func TestAllowedOriginsDefault(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	if got := AllowedOrigins(); len(got) != 0 {
		t.Fatalf("want empty (fail closed), got %v", got)
	}
}

func TestAllowedOriginsParsed(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.test.com, http://localhost:5173 ,")
	got := AllowedOrigins()
	if len(got) != 2 || got[0] != "https://app.test.com" || got[1] != "http://localhost:5173" {
		t.Fatalf("bad parse: %v", got)
	}
}

func TestAllowedOriginsWildcard(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")
	got := AllowedOrigins()
	if len(got) != 1 || got[0] != "*" {
		t.Fatalf("want [*], got %v", got)
	}
}
