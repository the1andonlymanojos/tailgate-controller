package tailscale

import (
	"context"
	"log"
	"net"
	"os"
	"testing"

	"tailscale.com/tsnet"
)

func StartTsnetNode(hostname, authKey string) (*tsnet.Server, net.Listener, error) {
	s := &tsnet.Server{
		Hostname:  "oauthdude",
		AuthKey:   authKey,
		Ephemeral: true,
	}

	if err := s.Start(); err != nil {
		return nil, nil, err
	}

	ln, err := s.Listen("tcp", ":8080")
	if err != nil {
		return nil, nil, err
	}

	log.Println("tsnet node started and listening on", ln.Addr())
	return s, ln, nil
}
func TestMintAuthKey(t *testing.T) {
	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	tailnet := os.Getenv("TAILNET_NAME") // e.g. example.com

	if clientID == "" || clientSecret == "" || tailnet == "" {
		t.Skip("OAUTH_CLIENT_ID, OAUTH_CLIENT_SECRET, or TAILNET_NAME env vars not set; skipping integration test")
	}

	tokenCache := NewTokenCache(clientID, clientSecret)

	ctx := context.Background()
	authKey, err := MintAuthKey(ctx, tokenCache, tailnet, "tag:example")
	if err != nil {
		t.Fatalf("MintAuthKey failed: %v", err)
	}

	if authKey == "" {
		t.Fatal("Expected non-empty auth key")
	}

	t.Logf("Minted auth key: %s", authKey)
	_, ln, err := StartTsnetNode("test-node", authKey)
	if err != nil {
		t.Fatalf("StartTsnetNode failed: %v", err)
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			t.Fatalf("Accept failed: %v", err)
		}
		conn.Close()
	}
}
