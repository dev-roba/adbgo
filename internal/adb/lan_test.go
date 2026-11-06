package adb

import (
	"net"
	"testing"
	"time"
)

func TestCIDRHosts24(t *testing.T) {
	hosts, err := CIDRHosts("192.168.1.0/24", 0)
	if err != nil {
		t.Fatalf("CIDRHosts: %v", err)
	}
	if len(hosts) != 254 {
		t.Fatalf("want 254 hosts, got %d", len(hosts))
	}
	if hosts[0] != "192.168.1.1" || hosts[253] != "192.168.1.254" {
		t.Fatalf("bad bounds: first=%s last=%s", hosts[0], hosts[len(hosts)-1])
	}
}

func TestCIDRHostsCap(t *testing.T) {
	hosts, err := CIDRHosts("10.0.0.0/16", 100)
	if err != nil {
		t.Fatalf("CIDRHosts: %v", err)
	}
	if len(hosts) != 100 {
		t.Fatalf("cap not applied: got %d", len(hosts))
	}
	if hosts[0] != "10.0.0.1" {
		t.Fatalf("bad start: %s", hosts[0])
	}
}

func TestCIDRHostsCarryAcrossOctets(t *testing.T) {
	hosts, err := CIDRHosts("192.168.0.0/23", 0)
	if err != nil {
		t.Fatalf("CIDRHosts: %v", err)
	}
	if len(hosts) != 510 {
		t.Fatalf("want 510 hosts, got %d", len(hosts))
	}
	if hosts[0] != "192.168.0.1" {
		t.Fatalf("bad start: %s", hosts[0])
	}
	if hosts[254] != "192.168.0.255" {
		t.Fatalf("want 192.168.0.255 at index 254, got %s", hosts[254])
	}
	if hosts[255] != "192.168.1.0" {
		t.Fatalf("octet carry broken: got %s", hosts[255])
	}
	if hosts[509] != "192.168.1.254" {
		t.Fatalf("bad end: %s", hosts[509])
	}
}

func TestCIDRHostsTinyNetwork(t *testing.T) {
	hosts, err := CIDRHosts("192.168.1.0/30", 0)
	if err != nil {
		t.Fatalf("CIDRHosts: %v", err)
	}
	if len(hosts) != 2 || hosts[0] != "192.168.1.1" || hosts[1] != "192.168.1.2" {
		t.Fatalf("bad /30 hosts: %v", hosts)
	}
}

func TestCIDRHostsRejectsIPv6(t *testing.T) {
	if _, err := CIDRHosts("2001:db8::/64", 0); err == nil {
		t.Fatal("expected error for IPv6 network")
	}
}

func TestDiscoverLANNetworks(t *testing.T) {
	nets := DiscoverLANNetworks(true)
	seen := map[string]bool{}
	for _, n := range nets {
		if seen[n.CIDR] {
			t.Fatalf("duplicate network %s", n.CIDR)
		}
		seen[n.CIDR] = true
		if n.CIDR == "" || n.Iface == "" || n.Source == "" {
			t.Fatalf("incomplete network record: %+v", n)
		}
		if _, _, err := net.ParseCIDR(n.CIDR); err != nil {
			t.Fatalf("bad CIDR %s: %v", n.CIDR, err)
		}
	}
	found := false
	for _, n := range nets {
		if n.Source == "hotspot" {
			found = true
		}
	}
	if !found {
		t.Fatal("hotspot ranges missing")
	}
}

func TestSweepPortStopped(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	// TEST-NET-1 (documentation range): nothing live, and the closed stop
	// channel must abort before any probe is even dispatched.
	hosts, err := CIDRHosts("192.0.2.0/29", 0)
	if err != nil {
		t.Fatalf("CIDRHosts: %v", err)
	}
	found := SweepPort(hosts, 5555, 100*time.Millisecond, 8, nil, stop)
	if len(found) != 0 {
		t.Fatalf("expected no targets after stop, got %v", found)
	}
}

