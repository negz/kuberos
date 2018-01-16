// +build integration

package kuberos

import (
	"context"
	"testing"

	oidc "github.com/coreos/go-oidc"
)

func TestOfflineAsScope(t *testing.T) {
	cases := []struct {
		name           string
		issuer         string
		offlineAsScope bool
	}{
		// TODO(negz): Call a mock provider instead of Google. oidc.NewProvider()
		// is the only way to instantiate a provider, and makes a real HTTP call.
		{
			name:           "Google",
			issuer:         "https://accounts.google.com",
			offlineAsScope: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p, err := oidc.NewProvider(context.Background(), tt.issuer)
			if err != nil {
				t.Fatalf("oidc.NewProvider(context.Background(), %v): %s", tt.issuer, err)
			}
			actual := OfflineAsScope(p)
			if tt.offlineAsScope != actual {
				t.Fatalf("OfflineAsScope(%v): wanted %v, got %v", p.Endpoint(), tt.offlineAsScope, actual)
			}
		})
	}
}
