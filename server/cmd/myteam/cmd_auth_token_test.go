package main

import "testing"

func TestValidatePersonalAccessToken(t *testing.T) {
	t.Run("accepts mul_ prefix", func(t *testing.T) {
		if err := validatePersonalAccessToken("mul_example_token_value"); err != nil {
			t.Fatalf("validatePersonalAccessToken() error = %v, want nil", err)
		}
	})

	t.Run("rejects unknown prefix", func(t *testing.T) {
		if err := validatePersonalAccessToken("xyz_example_token"); err == nil {
			t.Fatal("validatePersonalAccessToken() error = nil, want non-nil")
		}
	})
}
