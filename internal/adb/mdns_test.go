package adb

import (
	"encoding/binary"
	"net"
	"testing"
)

// appendName writes DNS labels and the root terminator.
func appendName(b []byte, labels ...string) []byte {
	for _, l := range labels {
		b = append(b, byte(len(l)))
		b = append(b, []byte(l)...)
	}
	return append(b, 0)
}

// containsLabel reports whether msg contains the wire-encoded label.
func containsLabel(msg []byte, label string) bool {
	n := len(label)
	for i := 0; i+n+1 <= len(msg); i++ {
		if msg[i] == byte(n) && string(msg[i+1:i+1+n]) == label {
			return true
		}
	}
	return false
}

func TestBuildMDNSQuery(t *testing.T) {
	q := buildMDNSQuery(MDNSConnectService)
	if len(q) < 20 {
		t.Fatalf("query suspiciously short: %d bytes", len(q))
	}
	if binary.BigEndian.Uint16(q[4:6]) != 1 {
		t.Fatal("qdcount != 1")
	}
	if binary.BigEndian.Uint16(q[len(q)-4:len(q)-2]) != 12 {
		t.Fatal("question type is not PTR")
	}
	if !containsLabel(q, "_adb-tls-connect") || !containsLabel(q, "local") {
		t.Fatal("query is missing service labels")
	}
}

func TestParseMDNSResponseAndCombine(t *testing.T) {
	var m []byte
	m = append(m, 0, 0, 0x84, 0x00) // id, flags: response + authoritative
	m = append(m, 0, 0)             // qdcount
	m = append(m, 0, 3)             // ancount = 3
	m = append(m, 0, 0, 0, 0) // nscount, arcount

	// Answer 1: SRV pixel._adb-tls-connect._tcp.local → android-1234.local:37459
	nameOff := len(m)
	m = appendName(m, "pixel", "_adb-tls-connect", "_tcp", "local")
	m = binary.BigEndian.AppendUint16(m, 33) // SRV
	m = binary.BigEndian.AppendUint16(m, 1)  // IN
	m = binary.BigEndian.AppendUint32(m, 120)
	var rd []byte
	rd = append(rd, 0, 0) // priority
	rd = append(rd, 0, 0) // weight
	rd = binary.BigEndian.AppendUint16(rd, 37459)
	rd = appendName(rd, "android-1234", "local")
	m = binary.BigEndian.AppendUint16(m, uint16(len(rd)))
	m = append(m, rd...)

	// Answer 2: A android-1234.local → 192.168.1.42
	m = appendName(m, "android-1234", "local")
	m = binary.BigEndian.AppendUint16(m, 1) // A
	m = binary.BigEndian.AppendUint16(m, 1)
	m = binary.BigEndian.AppendUint32(m, 120)
	m = binary.BigEndian.AppendUint16(m, 4)
	m = append(m, 192, 168, 1, 42)

	// Answer 3: A with a compression pointer back to answer 1's name
	m = append(m, 0xC0, byte(nameOff))
	m = binary.BigEndian.AppendUint16(m, 1)
	m = binary.BigEndian.AppendUint16(m, 1)
	m = binary.BigEndian.AppendUint32(m, 120)
	m = binary.BigEndian.AppendUint16(m, 4)
	m = append(m, 10, 1, 2, 3)

	recs := parseMDNSResponse(m)
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d", len(recs))
	}
	if recs[0].Type != 33 || recs[0].Port != 37459 {
		t.Fatalf("SRV wrong: %+v", recs[0])
	}
	if recs[0].Name != "pixel._adb-tls-connect._tcp.local" {
		t.Fatalf("SRV name wrong: %q", recs[0].Name)
	}
	if recs[0].Host != "android-1234.local" {
		t.Fatalf("SRV target wrong: %q", recs[0].Host)
	}
	if recs[1].Type != 1 || !recs[1].IP.Equal(net.IPv4(192, 168, 1, 42)) {
		t.Fatalf("A record wrong: %+v", recs[1])
	}
	if recs[2].Name != "pixel._adb-tls-connect._tcp.local" {
		t.Fatalf("compression pointer name wrong: %q", recs[2].Name)
	}
	if !recs[2].IP.Equal(net.IPv4(10, 1, 2, 3)) {
		t.Fatalf("pointer A record wrong: %v", recs[2].IP)
	}

	targets := combineMDNS(recs, MDNSConnectService)
	if len(targets) != 1 {
		t.Fatalf("want 1 target, got %d", len(targets))
	}
	if targets[0].Instance != "pixel" || targets[0].Addr != "192.168.1.42:37459" {
		t.Fatalf("target mismatch: %+v", targets[0])
	}
}

func TestParseMDNSResponseGarbage(t *testing.T) {
	// Short and malformed packets must return whatever parsed, never panic.
	if recs := parseMDNSResponse(nil); len(recs) != 0 {
		t.Fatalf("nil packet should yield no records, got %d", len(recs))
	}
	if recs := parseMDNSResponse([]byte{1, 2, 3}); len(recs) != 0 {
		t.Fatalf("short packet should yield no records, got %d", len(recs))
	}
	if recs := parseMDNSResponse([]byte{0, 0, 0, 0, 0, 0, 0, 9, 0, 0, 0, 0}); len(recs) != 0 {
		t.Fatalf("truncated answers should yield no records, got %d", len(recs))
	}
}

func TestCombineMDNSIgnoresOtherServices(t *testing.T) {
	recs := []mdnsRecord{
		{Name: "other._http._tcp.local", Type: 33, Port: 80, Host: "phone.local"},
		{Name: "phone.local", Type: 1, IP: net.IPv4(192, 168, 1, 5)},
	}
	if targets := combineMDNS(recs, MDNSConnectService); len(targets) != 0 {
		t.Fatalf("expected no targets, got %v", targets)
	}
}

func TestMergeMDNSTargets(t *testing.T) {
	a := []MDNSTarget{{Instance: "phone1", Addr: "192.168.1.10:40111"}}
	b := []MDNSTarget{{Instance: "phone1-dup", Addr: "192.168.1.10:40111"}, {Instance: "phone2", Addr: "192.168.1.11:37459"}}
	merged := MergeMDNSTargets(a, b)
	if len(merged) != 2 {
		t.Fatalf("want 2 targets after dedupe, got %d", len(merged))
	}
	if merged[0].Addr != "192.168.1.10:40111" || merged[1].Addr != "192.168.1.11:37459" {
		t.Fatalf("merged order wrong: %v", merged)
	}
}
