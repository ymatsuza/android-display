package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

func TestAdvertiseAndDiscover(t *testing.T) {
	// Start advertising
	svc, err := NewService("TestMac", 9000)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Stop()

	// Discover
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		t.Fatalf("failed to create resolver: %v", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	found := false

	go func() {
		for entry := range entries {
			if entry.Instance == "TestMac" {
				found = true
			}
		}
	}()

	// Allow time for the mDNS advertiser to register on the network.
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = resolver.Browse(ctx, ServiceType, "local.", entries)
	if err != nil {
		t.Fatalf("browse error: %v", err)
	}
	<-ctx.Done()

	if !found {
		t.Error("did not discover advertised service")
	}
}
