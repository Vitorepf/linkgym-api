package config

import (
	"os"
	"testing"
)

func TestApplyDotEnvDoesNotOverride(t *testing.T) {
	t.Setenv("PORT", "9999")
	ApplyDotEnv("PORT=8080\nENV=development\n")
	if os.Getenv("PORT") != "9999" {
		t.Fatalf("PORT = %s, want 9999", os.Getenv("PORT"))
	}
}

func TestApplyDotEnvSetsMissing(t *testing.T) {
	_ = os.Unsetenv("ENV")
	ApplyDotEnv("# comment\n\nENV=development\n")
	if os.Getenv("ENV") != "development" {
		t.Fatalf("ENV = %s", os.Getenv("ENV"))
	}
}
