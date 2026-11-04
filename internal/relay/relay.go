// Package relay implements a token-gated TCP rendezvous: two machines
// that can both dial out to the same server get their streams spliced
// byte-for-byte — so one machine can reach the other's adb port with no
// port forwarding, no cloud, and no VPN.
package relay

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8])
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

var (
	pendMu  sync.Mutex
	pending = map[string]net.Conn{}
)

func splice(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
}

// Serve runs the rendezvous: it waits for two clients presenting the
// same token and splices their streams. Blocks until error or Ctrl+C.
func Serve(port, token string) error {
	hash := tokenHash(token)
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	fmt.Println("relay: rendezvous listening on :" + port + " — waiting for both sides")
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func(c net.Conn) {
			r := bufio.NewReader(c)
			authLine, aerr := r.ReadString('\n')
			got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(authLine), "AUTH"))
			if aerr != nil || got != hash {
				fmt.Println("relay: rejected a connection (bad or missing token)")
				_ = c.Close()
				return
			}
			helloLine, herr := r.ReadString('\n')
			if herr != nil || !strings.HasPrefix(helloLine, "HELLO") {
				_ = c.Close()
				return
			}
			full := &bufConn{Conn: c, r: r}
			pendMu.Lock()
			other, ok := pending[hash]
			if ok {
				delete(pending, hash)
			} else {
				pending[hash] = full
			}
			pendMu.Unlock()
			if ok {
				fmt.Println("relay: pair complete — splicing streams")
				splice(other, full)
			} else {
				fmt.Println("relay: first side connected, waiting for the second...")
			}
		}(conn)
	}
}

// Expose dials the rendezvous and forwards a local TCP service (for
// example "localhost:5555") to whoever presents the same token.
func Expose(host, token, service string) error {
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(conn, "AUTH %s\nHELLO EXPOSE\n", tokenHash(token)); err != nil {
		_ = conn.Close()
		return err
	}
	svc, err := net.Dial("tcp", service)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("could not reach local service %s: %w", service, err)
	}
	fmt.Println("relay: connected to rendezvous, forwarding " + service)
	splice(conn, svc)
	fmt.Println("relay: session ended")
	return nil
}

// Connect dials the rendezvous and serves a local port that forwards
// into the tunnel: point any client at 127.0.0.1:<listenPort>.
func Connect(host, token string, listenPort int) error {
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(conn, "AUTH %s\nHELLO CONNECT\n", tokenHash(token)); err != nil {
		_ = conn.Close()
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(listenPort))
	if err != nil {
		_ = conn.Close()
		return err
	}
	fmt.Println("relay: tunnel ready — point your client at 127.0.0.1:" + strconv.Itoa(listenPort))
	c, err := ln.Accept()
	if err != nil {
		_ = conn.Close()
		return err
	}
	_ = ln.Close()
	splice(conn, c)
	fmt.Println("relay: session ended")
	return nil
}
