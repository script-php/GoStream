package modules

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateConfigRequiresCredentialPair(t *testing.T) {
	tests := []struct {
		name      string
		username  string
		password  string
		wantError bool
	}{
		{name: "disabled", wantError: false},
		{name: "configured", username: "admin", password: "secret", wantError: false},
		{name: "username only", username: "admin", wantError: true},
		{name: "password only", password: "secret", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateConfig(&IConfig{Username: test.username, Password: test.password})
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateConfig() error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestLoadConfigFromURLRejectsOversizedResponse(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), maxRemoteConfigSize+1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(payload)
	}))
	defer server.Close()

	_, err := LoadConfigFromURL(server.URL)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}
