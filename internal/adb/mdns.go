package adb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The mDNS service names adbd advertises for wireless debugging.
const (
	MDNSConnectService = "_adb-tls-connect._tcp"
	MDNSPairingService = "_adb-tls-pairing._tcp"
)

// mdnsGroup is the mDNS multicast group (224.0.0.251:5353).
var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// buildMDNSQuery builds a one-question PTR query for "<service>.local".
func buildMDNSQuery(service string) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0) // id
	b = binary.BigEndian.AppendUint16(b, 0) // flags
	b = binary.BigEndian.AppendUint16(b, 1) // qdcount
	b = binary.BigEndian.AppendUint16(b, 0) // ancount
	b = binary.BigEndian.AppendUint16(b, 0) // nscount
	b = binary.BigEndian.AppendUint16(b, 0) // arcount
	for _, label := range append(strings.Split(service, "."), "local") {
		if label == "" {
			continue
		}
		b = append(b, byte(len(label)))
		b = append(b, []byte(label)...)
	}
	b = append(b, 0)
	b = binary.BigEndian.AppendUint16(b, 12) // PTR
	b = binary.BigEndian.AppendUint16(b, 1)  // IN
	return b
}

// parseMDNSName decodes a possibly-compressed DNS name starting at off.
// It returns the decoded name and the offset just past the encoded name.
func parseMDNSName(msg []byte, off int, visited map[int]bool) (string, int, error) {
	var labels []string
	for {
		if off < 0 || off >= len(msg) {
			return "", 0, errors.New("truncated name")
		}
		b := int(msg[off])
		if b == 0 {
			off++
			break
		}
		if b&0xC0 == 0xC0 {
			if off+1 >= len(msg) {
				return "", 0, errors.New("truncated pointer")
			}
			ptr := int(b&0x3F)<<8 | int(msg[off+1])
			if visited[ptr] || ptr >= len(msg) {
				return "", 0, errors.New("bad pointer")
			}
			visited[ptr] = true
			sub, _, err := parseMDNSName(msg, ptr, visited)
			if err != nil {
				return "", 0, err
			}
			labels = append(labels, sub)
			return strings.Join(labels, "."), off + 2, nil
		}
		if b > 63 {
			return "", 0, errors.New("bad label length")
		}
		if off+1+b > len(msg) {
			return "", 0, errors.New("truncated label")
		}
		labels = append(labels, string(msg[off+1:off+1+b]))
		off += 1 + b
		if len(labels) > 128 {
			return "", 0, errors.New("too many labels")
		}
	}
	return strings.Join(labels, "."), off, nil
}

// mdnsRecord is one answer extracted from an mDNS packet.
type mdnsRecord struct {
	Name string
	Type uint16
	Ptr  string // PTR target (type 12)
	Port uint16 // SRV port (type 33)
	Host string // SRV target (type 33)
	IP   net.IP // A record (type 1)
}

// parseMDNSResponse extracts the answer records from an mDNS packet. It
// never fails hard on trailing garbage: whatever parsed cleanly is
// returned, so a noisy network cannot break discovery.
func parseMDNSResponse(msg []byte) []mdnsRecord {
	var recs []mdnsRecord
	if len(msg) < 12 {
		return recs
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	off := 12
	for i := 0; i < qd; i++ {
		_, noff, err := parseMDNSName(msg, off, map[int]bool{})
		if err != nil {
			return recs
		}
		off = noff + 4 // qtype + qclass
	}
	for i := 0; i < an; i++ {
		name, noff, err := parseMDNSName(msg, off, map[int]bool{})
		if err != nil {
			return recs
		}
		off = noff
		if off+10 > len(msg) {
			return recs
		}
		rtype := binary.BigEndian.Uint16(msg[off : off+2])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8 : off+10]))
		off += 10
		if off+rdlen > len(msg) {
			return recs
		}
		rec := mdnsRecord{Name: name, Type: rtype}
		switch rtype {
		case 12: // PTR
			if t, _, perr := parseMDNSName(msg, off, map[int]bool{}); perr == nil {
				rec.Ptr = t
			}
		case 33: // SRV
			if rdlen >= 7 {
				rec.Port = binary.BigEndian.Uint16(msg[off+4 : off+6])
				if h, _, perr := parseMDNSName(msg, off+6, map[int]bool{}); perr == nil {
					rec.Host = h
				}
			}
		case 1: // A
			if rdlen == 4 {
				rec.IP = net.IPv4(msg[off], msg[off+1], msg[off+2], msg[off+3])
			}
		}
		recs = append(recs, rec)
		off += rdlen
	}
	return recs
}

// combineMDNS turns parsed records into dialable targets for one service.
func combineMDNS(records []mdnsRecord, service string) []MDNSTarget {
	suffix := strings.ToLower(service + ".local")
	ips := map[string]string{}
	for _, r := range records {
		if r.Type == 1 && r.IP != nil {
			key := strings.ToLower(strings.TrimSuffix(r.Name, "."))
			ips[key] = r.IP.String()
		}
	}
	var out []MDNSTarget
	seen := map[string]bool{}
	for _, r := range records {
		if r.Type != 33 || r.Port == 0 {
			continue
		}
		owner := strings.ToLower(strings.TrimSuffix(r.Name, "."))
		if !strings.HasSuffix(owner, suffix) {
			continue
		}
		instance := strings.TrimSuffix(strings.TrimSuffix(owner, suffix), ".")
		hostKey := strings.ToLower(strings.TrimSuffix(r.Host, "."))
		ip := ips[hostKey]
		if ip == "" {
			continue // SRV without a resolved address — wait for the A record
		}
		addr := net.JoinHostPort(ip, strconv.Itoa(int(r.Port)))
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, MDNSTarget{Instance: instance, Addr: addr})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out
}

// QueryMDNSService sends a raw mDNS PTR query for "<service>.local" and
// collects answers for the given duration. It needs neither the adb mdns
// backend nor avahi: phones reply unicast to this ephemeral socket.
func QueryMDNSService(service string, wait time.Duration) []MDNSTarget {
	if wait <= 0 {
		wait = 2 * time.Second
	}
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil
	}
	defer conn.Close()
	query := buildMDNSQuery(service)
	if _, werr := conn.WriteTo(query, mdnsGroup); werr != nil {
		return nil
	}
	var records []mdnsRecord
	buf := make([]byte, 9000)
	deadline := time.Now().Add(wait)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		slice := remaining
		if slice > time.Second {
			slice = time.Second
		}
		_ = conn.SetReadDeadline(time.Now().Add(slice))
		n, _, rerr := conn.ReadFrom(buf)
		if n > 0 {
			records = append(records, parseMDNSResponse(buf[:n])...)
		}
		if rerr != nil && time.Until(deadline) > 0 {
			// keep poking so late advertisers get a chance to answer
			_, _ = conn.WriteTo(query, mdnsGroup)
		}
	}
	return combineMDNS(records, service)
}

// MergeMDNSTargets dedupes target lists by address, preserving order.
func MergeMDNSTargets(lists ...[]MDNSTarget) []MDNSTarget {
	seen := map[string]bool{}
	var out []MDNSTarget
	for _, l := range lists {
		for _, t := range l {
			if t.Addr == "" || seen[t.Addr] {
				continue
			}
			seen[t.Addr] = true
			out = append(out, t)
		}
	}
	return out
}

// DiscoverMDNSSafe is DiscoverMDNSDetailed with the error swallowed.
func DiscoverMDNSSafe() []MDNSTarget {
	targets, _ := DiscoverMDNSDetailed()
	return targets
}

// DiscoverMDNSAll merges the adb server's mDNS view with adbgo's own raw
// multicast query, so wireless-debugging discovery keeps working when
// the adb mdns backend is unavailable (bare distros without avahi, some
// packaged adb builds).
func DiscoverMDNSAll() []MDNSTarget {
	if targets := DiscoverMDNSSafe(); len(targets) > 0 {
		return targets
	}
	return QueryMDNSService(MDNSConnectService, 2*time.Second)
}

// FindPairingService locates the phone's _adb-tls-pairing service via a
// raw multicast query, preferring the instance matching our invented
// service name but accepting any pairing advertisement.
func FindPairingService(name string, wait time.Duration) (string, error) {
	targets := QueryMDNSService(MDNSPairingService, wait)
	want := strings.ToLower(name)
	var fallback string
	for _, t := range targets {
		if fallback == "" {
			fallback = t.Addr
		}
		if strings.Contains(strings.ToLower(t.Instance), want) {
			return t.Addr, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("no pairing service answered the raw mDNS query — is the phone still on the pairing screen?")
}
