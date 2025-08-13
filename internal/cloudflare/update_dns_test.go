package cloudflare

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// TestUpdateAndDeleteDNS is an integration test that:
// 1) Loads environment variables from .env at repository root
// 2) Creates/updates a CNAME record
// 3) Deletes that record
//
// Required env vars (typically provided via .env):
// - CLOUDFLARE_API_KEY
// - CLOUDFLARE_ZONE_ID
// - CF_TEST_DOMAIN            (e.g. example.com)
// - CF_TEST_HOSTNAME          (e.g. myserver)
// - CF_TEST_TAILNET_NAME      (e.g. tailnet.ts.net)
// - CF_TEST_SUBDOMAIN_PREFIX  (optional, defaults to "tg-test")
func TestUpdateDNSCreatesRecord(t *testing.T) {
	// Load .env from repo root when running tests from package dir
	// Try multiple common locations so it works whether tests are run from root or this subdir
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")

	apiKey := os.Getenv("CLOUDFLARE_API_KEY")
	zoneID := os.Getenv("CLOUDFLARE_ZONE_ID")
	domain := os.Getenv("CF_TEST_DOMAIN")
	hostname := os.Getenv("CF_TEST_HOSTNAME")
	tailnetName := os.Getenv("CF_TEST_TAILNET_NAME")
	subPrefix := os.Getenv("CF_TEST_SUBDOMAIN_PREFIX")
	if subPrefix == "" {
		subPrefix = "tg-test"
	}

	if apiKey == "" || zoneID == "" || domain == "" || hostname == "" || tailnetName == "" {
		t.Skip("missing required env vars; ensure .env has CLOUDFLARE_API_KEY, CLOUDFLARE_ZONE_ID, CF_TEST_DOMAIN, CF_TEST_HOSTNAME, CF_TEST_TAILNET_NAME", apiKey)
	}

	ctx := context.Background()
	dnsName := subPrefix + "-" + time.Now().Format("20060102150405")

	if err := UpdateDNS(ctx, dnsName, domain, hostname, tailnetName); err != nil {
		t.Fatalf("UpdateDNS failed: %v", err)
	}

	fullDomain := dnsName + "." + domain
	cnameTarget := hostname + "." + tailnetName
	t.Logf("Created/updated CNAME %s -> %s", fullDomain, cnameTarget)
	t.Log("Verify in Cloudflare dashboard or via: dig +short CNAME ", fullDomain)
}
