package adb

import (
	"fmt"
	"net"
	"sort"
	"time"
)

// LANSubnets returns the local /24 networks worth sweeping (from this
// machine's interfaces plus the classic Android hotspot ranges).
func LANSubnets() []string {
	prefixes := map[string]bool{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4[0] == 127 {
				continue
			}
			prefixes[fmt.Sprintf("%d.%d.%d", ip4[0], ip4[1], ip4[2])] = true
		}
	}
	prefixes["192.168.43"] = true // classic Android hotspot subnet
	prefixes["192.168.49"] = true // newer Android hotspot subnet
	list := make([]string, 0, len(prefixes))
	for p := range prefixes {
		list = append(list, p)
	}
	sort.Strings(list)
	return list
}

// ScanLANPorts sweeps the local /24 subnets for hosts with the given TCP
// port open (5555 = adbd in tcpip mode). Authorized use only: run this
// on networks you own or are allowed to scan.
func ScanLANPorts(port int, progress func(done, total int)) ([]string, error) {
	subnets := LANSubnets()
	var hosts []string
	for _, s := range subnets {
		for i := 1; i <= 254; i++ {
			hosts = append(hosts, fmt.Sprintf("%s.%d:%d", s, i, port))
		}
	}
	type result struct {
		target string
		open   bool
	}
	results := make(chan result, len(hosts))
	sem := make(chan struct{}, 64)
	for i, h := range hosts {
		h := h
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			conn, err := net.DialTimeout("tcp", h, 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				results <- result{target: h, open: true}
				return
			}
			results <- result{target: h}
		}()
		if progress != nil && (i+1)%64 == 0 {
			progress(i+1, len(hosts))
		}
	}
	var open []string
	for i := 0; i < len(hosts); i++ {
		r := <-results
		if r.open {
			open = append(open, r.target)
		}
	}
	if progress != nil {
		progress(len(hosts), len(hosts))
	}
	sort.Strings(open)
	return open, nil
}
