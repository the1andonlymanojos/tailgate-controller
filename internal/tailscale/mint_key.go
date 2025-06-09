package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	AuthURL = "https://api.tailscale.com/api/v2/tailnet/-/keys"
)

type MintKeyRequest struct {
	// Specify tags or other options your token requires
	Tag string `json:"tag,omitempty"`
	// ExpirySeconds int `json:"expirySeconds,omitempty"` // optional, if supported
}

type MintKeyResponse struct {
	Key string `json:"key"` // This is the auth key you want
}

func MintAuthKey(ctx context.Context, tokenCache *TokenCache, tailnet string, tag string) (string, error) {
	accessToken, err := tokenCache.GetToken(ctx)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"description": "dev access",
		"capabilities": map[string]interface{}{
			"devices": map[string]interface{}{
				"create": map[string]interface{}{
					"reusable":      true,
					"ephemeral":     true,
					"preauthorized": true,
					"tags":          []string{"tag:example", "tag:tailgate"},
				},
			},
		},
		"expirySeconds": 86400,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", AuthURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to mint auth key: %s\nbody: %s", resp.Status, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	key, ok := result["key"].(string)
	if !ok {
		return "", fmt.Errorf("auth key not found in response")
	}
	return key, nil
}
