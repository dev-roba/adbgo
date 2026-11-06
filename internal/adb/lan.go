package adb

import (
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LANNetwork is one IPv4 network this machine can reach directly.
type LANNetwork struct {
	CIDR   string // "192.168.1.0/24"
	Iface  string // "wlan0", or "hotspot" for the classic ranges
	Source string // "interface" or "hotspot"
}

// hotspotCIDRs are the ranges phone hotspot mode traditionally uses.
var hotspotCIDRs = []string{"192.168.43.0/24", "192.168.49.0/24", "172.20.10.0/28"}

// DiscoverLANNetworks enumerates the live IPv4 networks from this host's
// interfaces — skipping loopback, link-local and giant container bridges —
// plus the common phone-hotspot ranges when includeHotspots is set.
func DiscoverLANNetworks(includeHotspots bool) []LANNetwork {
	var nets []LANNetwork
	seen := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nets
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, aerr := ifc.Addrs()
		if aerr != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			ones, bits := ipn.Mask.Size()
			if bits != 32 || ones < 20 || ones > 30 {
				continue
			}
			cidr := (&net.IPNet{IP: ipn.IP.Mask(ipn.Mask), Mask: ipn.Mask}).String()
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			nets = append(nets, LANNetwork{CIDR: cidr, Iface: ifc.Name, Source: "interface"})
		}
	}
	if includeHotspots {
		for _, h := range hotspotCIDRs {
			if !seen[h] {
				seen[h] = true
				nets = append(nets, LANNetwork{CIDR: h, Iface: "hotspot", Source: "hotspot"})
			}
		}
	}
	sort.Slice(nets, func(i, j int) bool { return nets[i].CIDR < nets[j].CIDR })
	return nets
}

// CIDRHosts lists the usable host addresses of an IPv4 network (network
// and broadcast excluded), capped at maxHosts (<=0 means no cap).
func CIDRHosts(cidr string, maxHosts int) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("%s is not an IPv4 network", cidr)
	}
	base := ipnet.IP.To4()
	if base == nil {
		return nil, fmt.Errorf("%s is not an IPv4 network", cidr)
	}
	limit := (1 << uint(32-ones)) - 2
	if maxHosts > 0 && limit > maxHosts {
		limit = maxHosts
	}
	var hosts []string
	cur := [4]byte{base[0], base[1], base[2], base[3]}
	for i := 0; i < limit; i++ {
		for k := 3; k >= 0; k-- {
			cur[k]++
			if cur[k] != 0 {
				break
			}
		}
		hosts = append(hosts, net.IP(cur[:]).String())
	}
	return hosts, nil
}

// SweepPort dials one TCP port on many hosts in parallel and returns the
// hosts that answered, sorted. progress (optional) reports (open, done,
// total) as probes complete; closing stop cancels the sweep early.
// Authorized use only: scan networks you own.
func SweepPort(hosts []string, port int, timeout time.Duration, concurrency int, progress func(open, done, total int), stop <-chan struct{}) []string {
	if timeout <= 0 {
		timeout = 400 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 256
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var open []string
	var done int32
	var openN int32
	sem := make(chan struct{}, concurrency)
	for _, host := range hosts {
		if stop != nil {
			select {
			case <-stop:
				wg.Wait()
				return finishSweep(&mu, open)
			default:
			}
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(h, strconv.Itoa(port)), timeout)
			if err == nil {
				_ = conn.Close()
				mu.Lock()
				open = append(open, h)
				mu.Unlock()
				atomic.AddInt32(&openN, 1)
			}
			if progress != nil {
				d := atomic.AddInt32(&done, 1)
				progress(int(atomic.LoadInt32(&openN)), int(d), len(hosts))
			}
		}(host)
	}
	wg.Wait()
	return finishSweep(&mu, open)
}

// finishSweep copies and sorts a sweep result under its mutex.
func finishSweep(mu *sync.Mutex, open []string) []string {
	mu.Lock()
	out := append([]string(nil), open...)
	mu.Unlock()
	sort.Strings(out)
	return out
}

// ScanLANPorts sweeps every network this machine sits on (plus the
// classic hotspot ranges) for hosts with the given TCP port open and
// returns "ip:port" targets. Authorized use only: scan networks you own.
func ScanLANPorts(port int, progress func(done, total int)) ([]string, error) {
	var hosts []string
	seen := map[string]bool{}
	for _, n := range DiscoverLANNetworks(true) {
		hs, err := CIDRHosts(n.CIDR, 1024)
		if err != nil {
			continue
		}
		for _, h := range hs {
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}
	prog := func(open, done, total int) {
		if progress != nil {
			progress(done, total)
		}
	}
	found := SweepPort(hosts, port, 400*time.Millisecond, 256, prog, nil)
	out := make([]string, len(found))
	for i, h := range found {
		out[i] = net.JoinHostPort(h, strconv.Itoa(port))
	}
	return out, nil
}

// DeepScanHost hunts adbd listeners on one host: port 5555 plus the
// random 30000+ range that Android 11+ wireless debugging rotates
// through. Authorized use only: scan devices you own.
func DeepScanHost(ip string, timeout time.Duration, concurrency int, progress func(open, done, total int), stop <-chan struct{}) []int {
	candidates := []int{5555}
	for p := 30000; p <= 65535; p++ {
		candidates = append(candidates, p)
	}
	if timeout <= 0 {
		timeout = 350 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 512
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var open []int
	var done int32
	var openN int32
	sem := make(chan struct{}, concurrency)
	for _, port := range candidates {
		if stop != nil {
			select {
			case <-stop:
				wg.Wait()
				mu.Lock()
				out := append([]int(nil), open...)
				mu.Unlock()
				sort.Ints(out)
				return out
			default:
			}
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(p int) {
			defer wg.Done()
			defer func() { <-sem }()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(p)), timeout)
			if err == nil {
				_ = conn.Close()
				mu.Lock()
				open = append(open, p)
				mu.Unlock()
				atomic.AddInt32(&openN, 1)
			}
			if progress != nil {
				d := atomic.AddInt32(&done, 1)
				progress(int(atomic.LoadInt32(&openN)), int(d), len(candidates))
			}
		}(port)
	}
	wg.Wait()
	mu.Lock()
	out := append([]int(nil), open...)
	mu.Unlock()
	sort.Ints(out)
	return out
}

// DefaultGateway returns the default-route gateway IP ("" if unknown).
func DefaultGateway() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	for _, f := range strings.Fields(string(out)) {
		if net.ParseIP(f) != nil {
			return f
		}
	}
	return ""
}

// ADBDProbe is what adb really thinks about a LAN target after trying it.
type ADBDProbe struct {
	Addr   string
	State  string // device / offline / unauthorized / ...
	Model  string
	IsADBD bool
	Note   string
}

// ProbeTarget runs `adb connect` on a LAN target and reports what the
// device really is, including a human hint when it is not usable.
func ProbeTarget(addr string) ADBDProbe {
	p := ADBDProbe{Addr: addr}
	out, err := RunRaw("connect", addr)
	if err != nil {
		p.Note = "adb failed: " + err.Error()
		return p
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "cannot connect") || strings.Contains(low, "connection refused") {
		p.Note = "nothing answered on " + addr
		return p
	}
	devices, derr := ListDevices()
	if derr != nil {
		p.Note = "adb devices failed: " + derr.Error()
		return p
	}
	for _, d := range devices {
		if d.Serial != addr {
			continue
		}
		p.IsADBD = true
		p.State = d.Status
		p.Model = d.Model
		switch d.Status {
		case "device":
			p.Note = "ready — usable right now"
		case "offline":
			p.Note = "adbd answered but stays 'offline' — on wireless debugging this usually means: pair first (adbgo pairqr)"
		case "unauthorized":
			p.Note = "adbd wants approval — accept the debugging dialog on the phone (or it is paired to another machine)"
		default:
			p.Note = "adbd state '" + d.Status + "'"
		}
		return p
	}
	if strings.Contains(low, "already connected") {
		p.IsADBD = true
		p.State = "device"
		p.Model = modelOf(addr)
		p.Note = "already connected and usable"
		return p
	}
	p.Note = "no adb device appeared (adb said: " + strings.TrimSpace(out) + ")"
	return p
}

// ConnectReliable runs `adb connect` with retries and verification —
// phones sometimes drop the very first attempt right after a network
// switch or wake-up.
func ConnectReliable(addr string, attempts int) (string, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var last string
	for i := 0; i < attempts; i++ {
		_, err := ConnectDirect(addr)
		if err == nil && IsConnected(addr) {
			return addr, nil
		}
		if err != nil {
			last = err.Error()
		} else {
			last = "device did not reach 'device' state in time"
		}
		p := ProbeTarget(addr)
		if p.State == "device" {
			return addr, nil
		}
		if p.Note != "" {
			last = p.Note
		}
		if i < attempts-1 {
			time.Sleep(time.Duration(i+1) * 300 * time.Millisecond)
		}
	}
	return "", fmt.Errorf("adb connect %s failed: %s", addr, last)
}

// dialReason turns a dial error into a short human hint.
func dialReason(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "refused"):
		return "connection refused — nothing listens on that port (adbd off, or the port moved)"
	case strings.Contains(s, "timeout") || strings.Contains(s, "i/o timeout"):
		return "no response — wrong network, firewall, or the phone is asleep"
	case strings.Contains(s, "unreachable"):
		return "network unreachable — this machine has no route to that subnet"
	default:
		return err.Error()
	}
}

// DiagnoseTarget inspects a LAN target and returns human-readable
// findings, so "why won't this connect" has a real answer.
func DiagnoseTarget(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		host = addr
		port = "5555"
		addr = net.JoinHostPort(host, port)
	}
	var out []string
	start := time.Now()
	conn, derr := net.DialTimeout("tcp", addr, 800*time.Millisecond)
	latency := time.Since(start)
	if derr != nil {
		out = append(out, "✗ port "+port+" on "+host+" is unreachable: "+dialReason(derr))
		out = append(out, "  → wireless debugging rotates its port; find the live one with `adbgo lan mdns` or `adbgo lan deep "+host+"`")
		return out
	}
	_ = conn.Close()
	out = append(out, fmt.Sprintf("✓ port %s OPEN (TCP handshake in %d ms)", port, latency.Milliseconds()))
	p := ProbeTarget(addr)
	if p.IsADBD {
		out = append(out, fmt.Sprintf("✓ adbd confirmed — state '%s', model '%s'", p.State, p.Model))
		out = append(out, "  → "+p.Note)
		if p.State == "device" {
			out = append(out, "  → ready to use: adbgo connect "+addr)
		}
	} else {
		out = append(out, "✗ "+addr+" is open but adb cannot use it: "+p.Note)
		out = append(out, "  → if this is a phone with wireless debugging, its real adb port is random — try `adbgo lan deep "+host+"`")
	}
	if targets, merr := DiscoverMDNSDetailed(); merr == nil {
		for _, t := range targets {
			if strings.HasPrefix(t.Addr, host+":") {
				out = append(out, "✓ adb's mDNS backend advertises this phone at "+t.Addr)
			}
		}
	}
	for _, t := range QueryMDNSService(MDNSConnectService, 2*time.Second) {
		if strings.HasPrefix(t.Addr, host+":") {
			out = append(out, "✓ raw mDNS sees this phone: "+t.Instance+" → "+t.Addr)
		}
	}
	for _, e := range LoadKnown().Endpoints {
		if e.Addr == addr {
			out = append(out, "ℹ saved as known phone '"+e.Name+"' (last seen "+e.When+")")
		}
	}
	return out
}
