package main

import "testing"

func TestWorkerBinaryCompiles(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("nothing to run in short mode")
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("CSW_TEST_KEY", "")
	if got := envOrDefault("CSW_TEST_KEY", "fallback"); got != "fallback" {
		t.Fatalf("empty env should fall back, got %q", got)
	}
	t.Setenv("CSW_TEST_KEY", "value")
	if got := envOrDefault("CSW_TEST_KEY", "fallback"); got != "value" {
		t.Fatalf("set env should win, got %q", got)
	}
}

func TestEnvOrDefaultInt(t *testing.T) {
	t.Setenv("CSW_TEST_INT", "")
	if got := envOrDefaultInt("CSW_TEST_INT", 7); got != 7 {
		t.Fatalf("empty env should fall back, got %d", got)
	}
	t.Setenv("CSW_TEST_INT", "42")
	if got := envOrDefaultInt("CSW_TEST_INT", 7); got != 42 {
		t.Fatalf("parsed int should win, got %d", got)
	}
	t.Setenv("CSW_TEST_INT", "not-a-number")
	if got := envOrDefaultInt("CSW_TEST_INT", 7); got != 7 {
		t.Fatalf("bad int should fall back, got %d", got)
	}
}
