package main

import (
	"os"
	"testing"
)

func TestApplyDotEnvDoesNotOverride(t *testing.T) {
	t.Setenv("PORT", "9999")
	applyDotEnv("PORT=8080\nENV=development\n")
	if os.Getenv("PORT") != "9999" {
		t.Fatalf("PORT = %s, want 9999", os.Getenv("PORT"))
	}
}

func TestApplyDotEnvSetsMissing(t *testing.T) {
	t.Setenv("ENV", "")
	_ = os.Unsetenv("ENV")
	applyDotEnv("# comment\n\nENV=development\n")
	if os.Getenv("ENV") != "development" {
		t.Fatalf("ENV = %s", os.Getenv("ENV"))
	}
}
