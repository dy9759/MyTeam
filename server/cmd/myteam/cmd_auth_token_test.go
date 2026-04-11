package main

import "testing"

func TestValidatePersonalAccessToken(t *testing.T) {
	t.Run("accepts myteam prefix", func(t *testing.T) {
		if err := validatePersonalAccessToken("myt_example_token"); err != nil {
			t.Fatalf("validatePersonalAccessToken() error = %v, want nil", err)
		}
	})

	t.Run("rejects legacy prefix", func(t *testing.T) {
		if err := validatePersonalAccessToken("mul_example_token"); err == nil {
			t.Fatal("validatePersonalAccessToken() error = nil, want non-nil")
		}
	})
}
