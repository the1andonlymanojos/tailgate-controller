package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type CFListResponse struct {
	Success bool          `json:"success"`
	Errors  []interface{} `json:"errors"`
	Result  []DNSRecord   `json:"result"`
}

type CFWriteResponse struct {
	Success bool          `json:"success"`
	Errors  []interface{} `json:"errors"`
	Result  DNSRecord     `json:"result"`
}

func UpdateDNS(ctx context.Context, dnsName string, domain string, hostname string, tailnetName string) error {
	logger := logf.FromContext(ctx)
	apiKey := os.Getenv("CLOUDFLARE_API_KEY")
	zoneID := os.Getenv("CLOUDFLARE_ZONE_ID")
	fmt.Println("CLOUDFLARE_API_KEY: ", apiKey)

	if apiKey == "" || zoneID == "" {
		fmt.Println("CLOUDFLARE_API_KEY: ", apiKey)
		fmt.Println("CLOUDFLARE_ZONE_ID: ", zoneID)
		logger.Error(fmt.Errorf("missing CLOUDFLARE_API_KEY or CLOUDFLARE_ZONE_ID"), "Missing CLOUDFLARE_API_KEY or CLOUDFLARE_ZONE_ID")
		return fmt.Errorf("missing CLOUDFLARE_API_KEY or CLOUDFLARE_ZONE_ID")
	}
	authHeader := "Bearer " + apiKey

	fullDomain := fmt.Sprintf("%s.%s", dnsName, domain)

	client := http.DefaultClient

	// STEP 1: Check for existing record
	checkURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=CNAME&name=%s", zoneID, fullDomain)
	req, _ := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	req.Header.Set("Authorization", authHeader)

	resp, err := client.Do(req)
	fmt.Println("resp: ", resp)
	if err != nil {
		return fmt.Errorf("failed to fetch existing records: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println("body: ", string(body))
	var listResp CFListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return fmt.Errorf("error decoding list response: %w\nbody: %s", err, string(body))
	}
	fmt.Println("listResp: ", listResp)

	forwardingURL := fmt.Sprintf("%s.%s", hostname, tailnetName)

	fmt.Println("Forwarding URL: ", forwardingURL)
	record := DNSRecord{
		Type:    "CNAME",
		Name:    fullDomain,
		Content: forwardingURL,
		TTL:     60,
		Proxied: false,
	}

	var method string
	var url string

	if len(listResp.Result) > 0 {
		// Record exists → update it
		existing := listResp.Result[0]
		url = fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, existing.ID)
		method = "PUT"
	} else {
		// Record doesn't exist → create it
		url = fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", zoneID)
		method = "POST"
	}

	payload, _ := json.Marshal(record)
	req2, _ := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(payload))
	req2.Header.Set("Authorization", authHeader)
	resp2, err := client.Do(req2)
	//fmt.Println("resp2: ", resp2)
	if err != nil {
		return fmt.Errorf("failed to %s record: %w", method, err)
	}
	defer resp2.Body.Close()

	respBody, _ := io.ReadAll(resp2.Body)
	fmt.Println("respBody: ", string(respBody))
	var writeResp CFWriteResponse
	if err := json.Unmarshal(respBody, &writeResp); err != nil {
		return fmt.Errorf("error decoding write response: %w\nbody: %s", err, string(respBody))
	}

	if !writeResp.Success {
		logger.Error(fmt.Errorf("cloudflare API %s failed: %s", method, string(respBody)), "cloudflare API failed")
		return fmt.Errorf("cloudflare API %s failed: %s", method, string(respBody))
	}
	fmt.Println("writeResp: ", writeResp)

	return nil
}

func DeletDNS(ctx context.Context, dnsName string, domain string, hostname string, tailnetName string) error {
	logger := logf.FromContext(ctx)
	apiKey := os.Getenv("CLOUDFLARE_API_KEY")
	zoneID := os.Getenv("CLOUDFLARE_ZONE_ID")
	fmt.Println("CLOUDFLARE_API_KEY: ", apiKey)

	if apiKey == "" || zoneID == "" {
		fmt.Println("CLOUDFLARE_API_KEY: ", apiKey)
		fmt.Println("CLOUDFLARE_ZONE_ID: ", zoneID)
		logger.Error(fmt.Errorf("missing CLOUDFLARE_API_KEY or CLOUDFLARE_ZONE_ID"), "Missing CLOUDFLARE_API_KEY or CLOUDFLARE_ZONE_ID")
		return fmt.Errorf("missing CLOUDFLARE_API_KEY or CLOUDFLARE_ZONE_ID")
	}

	authHeader := "Bearer " + apiKey

	fullDomain := fmt.Sprintf("%s.%s", dnsName, domain)

	client := http.DefaultClient

	checkURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=CNAME&name=%s", zoneID, fullDomain)
	req, _ := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	req.Header.Set("Authorization", authHeader)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch existing records: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var listResp CFListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return fmt.Errorf("error decoding list response: %w\nbody: %s", err, string(body))
	}

	if len(listResp.Result) == 0 {
		logger.Info("No record found", "dnsName", dnsName, "domain", domain)
		return nil
	}

	existing := listResp.Result[0]
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, existing.ID)
	method := "DELETE"

	req, _ = http.NewRequestWithContext(ctx, method, url, nil)
	req.Header.Set("Authorization", authHeader)

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var writeResp CFWriteResponse
	if err := json.Unmarshal(respBody, &writeResp); err != nil {
		return fmt.Errorf("error decoding write response: %w\nbody: %s", err, string(respBody))
	}

	if !writeResp.Success {
		logger.Error(fmt.Errorf("cloudflare API %s failed: %s", method, string(respBody)), "cloudflare API failed")
		return fmt.Errorf("cloudflare API %s failed: %s", method, string(respBody))
	}

	return nil
}
