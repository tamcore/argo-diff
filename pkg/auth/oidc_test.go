package auth

import (
	"testing"
)

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantToken  string
		wantErr    bool
	}{
		{"valid bearer token", "Bearer abc123", "abc123", false},
		{"valid bearer token with extra spaces", "Bearer  abc123  ", "abc123", false},
		{"case insensitive bearer", "bearer abc123", "abc123", false},
		{"case insensitive BEARER", "BEARER abc123", "abc123", false},
		{"missing header", "", "", true},
		{"wrong scheme", "Basic abc123", "", true},
		{"missing token", "Bearer", "", true},
		{"missing token with space", "Bearer ", "", true},
		{"invalid format", "abc123", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := ExtractBearerToken(tt.authHeader)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractBearerToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && token != tt.wantToken {
				t.Errorf("ExtractBearerToken() = %v, want %v", token, tt.wantToken)
			}
		})
	}
}
