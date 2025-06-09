package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

/*
DeleteDevice deletes a device from the tailnet.

	curl https://api.tailscale.com/api/v2/device/ \
	  --request DELETE \
	  --header 'Authorization: Bearer YOUR_SECRET_TOKEN'
*/
func DeleteDevice(ctx context.Context, tokenCache *TokenCache, deviceID string) error {
	accessToken, err := tokenCache.GetToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.tailscale.com/api/v2/device/%s", deviceID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete device: %s", resp.Status)
	}

	return nil
}

type Device struct {
	NodeID   string `json:"nodeId"`
	Hostname string `json:"hostname"`
}

type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

func GetAllDevices(ctx context.Context, tokenCache *TokenCache, tailnet string) ([]Device, error) {
	logger := logf.FromContext(ctx)
	accessToken, err := tokenCache.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/devices", tailnet)
	logger.Info("URL", "url", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get devices: %s", resp.Status)
	}

	var result DevicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Devices, nil
}
