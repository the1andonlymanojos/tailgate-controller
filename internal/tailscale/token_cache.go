package tailscale

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

type TokenCache struct {
	token        *oauth2.Token
	expiresAt    time.Time
	mutex        sync.Mutex
	client       *http.Client
	clientID     string
	clientSecret string
}

func NewTokenCache(clientID, clientSecret string) *TokenCache {
	conf := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     "https://api.tailscale.com/api/v2/oauth/token",
		Scopes:       []string{"auth_keys", "devices:core"},
	}

	return &TokenCache{
		client:       conf.Client(context.Background()),
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

func (tc *TokenCache) GetToken(ctx context.Context) (string, error) {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	if tc.token != nil && time.Until(tc.token.Expiry) > 5*time.Minute {
		return tc.token.AccessToken, nil
	}

	conf := &clientcredentials.Config{
		ClientID:     tc.clientID,
		ClientSecret: tc.clientSecret,
		TokenURL:     "https://api.tailscale.com/api/v2/oauth/token",
		Scopes:       []string{"auth_keys", "devices:core"},
	}

	tok, err := conf.Token(ctx)
	fmt.Println("tok", tok)
	if err != nil {
		return "", err
	}

	tc.token = tok
	tc.expiresAt = tok.Expiry
	return tok.AccessToken, nil
}
