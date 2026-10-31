// Package share runs a small token-gated web server that lets an Android
// phone connect itself: adbgo prints a link (and QR code), the phone opens
// the page, its user enters the pairing port + code shown in the phone's
// Wireless debugging screen, and adbgo pairs and connects from there.
// In WAN mode the page is exposed through a free pinggy.io SSH tunnel so
// it works from anywhere on the internet.
package share

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dev-roba/adbgo/internal/adb"
	"github.com/dev-roba/adbgo/internal/qr"
	"github.com/dev-roba/adbgo/internal/ui"
)

// state is the shared connection state shown on the phone page.
type state struct {
	mu    sync.Mutex
	stage string
	msg   string
}

func (s *state) set(stage, msg string) {
	s.mu.Lock()
	s.stage = stage
	s.msg = msg
	s.mu.Unlock()
}

func (s *state) get() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stage, s.msg
}

// server holds the per-session share server.
type server struct {
	token  string
	mode   string
	st     *state
	seenMu sync.Mutex
	seenIP map[string]bool
}

func (s *server) checkToken(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("t") != s.token {
		http.Error(w, "invalid token", http.StatusForbidden)
		return false
	}
	return true
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	wan := "false"
	if s.mode == "wan" {
		wan = "true"
	}
	html := strings.ReplaceAll(pageHTML, "__TOKEN__", s.token)
	html = strings.ReplaceAll(html, "__WAN__", wan)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkToken(w, r) {
		return
	}
	ip := remoteIP(r)
	s.seenMu.Lock()
	if !s.seenIP[ip] {
		s.seenIP[ip] = true
		s.seenMu.Unlock()
		ui.PrintInfo("📱 A phone opened the link from " + ip)
	} else {
		s.seenMu.Unlock()
	}
	stage, msg := s.st.get()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"stage": stage, "message": msg, "ip": ip})
}

func (s *server) handlePair(w http.ResponseWriter, r *http.Request) {
	if !s.checkToken(w, r) {
		return
	}
	var req struct {
		Port string `json:"port"`
		Code string `json:"code"`
		IP   string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip := req.IP
	if ip == "" {
		ip = remoteIP(r)
	}
	ui.PrintInfo("🔐 Pairing request from the phone (" + ip + ":" + req.Port + ")...")
	if err := adb.PairWireless(ip+":"+req.Port, req.Code); err != nil {
		ui.PrintError("Pairing failed: " + err.Error())
		s.st.set("pair", "Pairing failed — check the code and port, then try again.")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	ui.PrintSuccess("Paired with " + ip)
	s.st.set("connect", "Paired! Now enter the main port.")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "paired"})
}

func (s *server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if !s.checkToken(w, r) {
		return
	}
	var req struct {
		Port string `json:"port"`
		IP   string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip := req.IP
	if ip == "" {
		ip = remoteIP(r)
	}
	target := ip + ":" + req.Port
	ui.PrintInfo("Connecting to " + target + "...")
	if err := adb.ConnectDirect(target); err != nil {
		ui.PrintError("Connect failed: " + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	ui.PrintSuccess("✅ Phone connected: " + target)
	s.st.set("done", "Connected")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "connected"})
}

// Run starts the sharing server in mode "lan" or "wan" and blocks until
// the user presses Enter.
func Run(mode string) {
	if mode != "wan" {
		mode = "lan"
	}

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		ui.PrintError("Could not start server: " + err.Error())
		return
	}
	port := ln.Addr().(*net.TCPAddr).Port
	token := newToken()

	url := fmt.Sprintf("http://%s:%d/s/%s", lanIP(), port, token)
	var sshCmd *exec.Cmd
	if mode == "wan" {
		ui.PrintInfo("Opening internet tunnel via pinggy.io (free, no account)...")
		publicURL, cmd, terr := startPinggy(port)
		if terr != nil {
			_ = ln.Close()
			ui.PrintError(terr.Error())
			ui.PrintWarning("Check that 'ssh' is installed and you are online — or use the LAN option.")
			return
		}
		sshCmd = cmd
		url = strings.TrimSuffix(publicURL, "/") + "/s/" + token
	}

	srv := &server{token: token, mode: mode, st: &state{stage: "pair"}, seenIP: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/s/"+token+"/", srv.handlePage)
	mux.HandleFunc("/s/"+token, srv.handlePage)
	mux.HandleFunc("/api/status", srv.handleStatus)
	mux.HandleFunc("/api/pair", srv.handlePair)
	mux.HandleFunc("/api/connect", srv.handleConnect)
	httpSrv := &http.Server{Handler: mux}
	go func() { _ = httpSrv.Serve(ln) }()

	fmt.Println()
	fmt.Println(ui.TitleStyle.Render("🔗 Phone Connect Link (" + strings.ToUpper(mode) + ")"))
	fmt.Println()
	fmt.Println("  1. Send this link to the phone (or let the phone scan the QR):")
	fmt.Println()
	fmt.Println("     " + url)
	fmt.Println()
	if mat, qerr := qr.Encode(url); qerr == nil {
		fmt.Println(qr.RenderANSI(mat))
	} else {
		ui.PrintWarning("Could not render QR: " + qerr.Error())
	}
	fmt.Println("  2. The phone opens the page and enters the pairing port + code")
	fmt.Println("     from Settings → Developer options → Wireless debugging.")
	fmt.Println("  3. adbgo pairs and connects — status updates appear here.")
	fmt.Println()
	fmt.Println(ui.DimStyle.Render("Waiting... press Enter in this terminal to stop sharing."))

	ui.ReadLine()
	_ = httpSrv.Close()
	if sshCmd != nil && sshCmd.Process != nil {
		_ = sshCmd.Process.Kill()
	}
	ui.PrintInfo("Sharing stopped.")
}

// newToken returns a random session token.
func newToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "adbgo123"
	}
	return hex.EncodeToString(b)
}

// lanIP returns the LAN IP of this machine (UDP dial trick, no packets sent).
func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return addr.IP.String()
		}
	}
	return "127.0.0.1"
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var pinggyRe = regexp.MustCompile(`https?://[0-9A-Za-z-]+(?:.[0-9A-Za-z-]+)*.pinggy.(?:link|io)`)

// startPinggy opens a free reverse tunnel through pinggy.io using plain
// ssh (no account needed) and returns the public URL and the ssh process.
func startPinggy(localPort int) (string, *exec.Cmd, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return "", nil, fmt.Errorf("ssh not found in PATH (required for the internet tunnel)")
	}
	cmd := exec.Command("ssh", "-p", "443",
		"-R", fmt.Sprintf("0:localhost:%d", localPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "ServerAliveInterval=30",
		"free.pinggy.io")
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}

	urlCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if u := pinggyRe.FindString(sc.Text()); u != "" {
				urlCh <- u
				return
			}
		}
	}()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if u := pinggyRe.FindString(sc.Text()); u != "" {
				urlCh <- u
				return
			}
		}
	}()

	select {
	case u := <-urlCh:
		return u, cmd, nil
	case <-time.After(20 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", nil, fmt.Errorf("tunnel did not come up within 20s (offline or blocked?)")
	}
}

// pageHTML is the mobile-friendly page served to the phone.
const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>adbgo — connect this phone</title>
<style>
body{font-family:system-ui,sans-serif;background:#0f1117;color:#e6e6e6;margin:0;padding:24px}
.card{max-width:480px;margin:0 auto;background:#1a1d27;border-radius:14px;padding:24px}
h1{font-size:1.3rem;margin:0 0 8px}p{line-height:1.5;color:#a8b0c0}
ol{padding-left:20px}li{margin:10px 0;line-height:1.5}
input{width:100%;box-sizing:border-box;padding:12px;border-radius:8px;border:1px solid #3a3f52;background:#0f1117;color:#fff;font-size:1rem;margin:6px 0 14px}
button{width:100%;padding:14px;border:0;border-radius:8px;background:#4f8cff;color:#fff;font-size:1rem;font-weight:600}
.ok{color:#4ade80}.err{color:#f87171}.badge{display:inline-block;background:#2a2f3f;border-radius:6px;padding:2px 8px;font-size:.85rem}
.hidden{display:none}
#err{white-space:pre-wrap;margin-top:12px}
#hint{background:#3b2f14;border-radius:8px;padding:12px;margin-bottom:12px;line-height:1.5;color:#a8b0c0}
</style></head><body><div class="card">
<h1>📱 adbgo connect</h1>
<p>This page was generated by <b>adbgo</b> on a computer nearby. Follow the steps to connect this phone for wireless debugging.</p>
<div id="hint" class="hidden"></div>
<div id="pairbox">
<ol>
<li>Open <b>Settings → Developer options → Wireless debugging</b> and switch it on.</li>
<li>Tap <b>Pair device with pairing code</b>.</li>
<li>Enter the <b>port</b> and the <b>6-digit code</b> shown on this phone below.</li>
</ol>
<input id="pport" inputmode="numeric" placeholder="Pairing port (e.g. 37281)">
<input id="pcode" inputmode="numeric" placeholder="6-digit pairing code">
<button onclick="doPair()">Pair this phone</button>
</div>
<div id="connectbox" class="hidden">
<ol>
<li>Go back to the <b>Wireless debugging</b> screen.</li>
<li>Enter the main port shown next to the IP address.</li>
</ol>
<input id="cport" inputmode="numeric" placeholder="Main port (e.g. 40123)">
<button onclick="doConnect()">Connect</button>
</div>
<div id="donebox" class="hidden"><h2 class="ok">✅ Connected!</h2><p>This phone is now paired and connected to the computer running adbgo. You can close this page.</p></div>
<p id="err" class="err"></p>
<p class="badge" id="stg"></p>
</div>
<script>
var TOKEN = "__TOKEN__";
var WAN = __WAN__;
if (WAN) {
  var h = document.getElementById('hint');
  h.classList.remove('hidden');
  h.innerHTML = '🌍 <b>Internet mode.</b> The computer is not on this network. Pairing only works if this network allows incoming connections; if it fails, use the hotspot fallback: enable this phone\u2019s <b>hotspot</b>, join it from the computer, then use adbgo\u2019s LAN share.';
}
function show(stage) {
  document.getElementById('pairbox').classList.toggle('hidden', stage !== 'pair');
  document.getElementById('connectbox').classList.toggle('hidden', stage !== 'connect');
  document.getElementById('donebox').classList.toggle('hidden', stage !== 'done');
  document.getElementById('stg').textContent = 'status: ' + stage;
}
async function poll() {
  try {
    var r = await fetch('/api/status?t=' + TOKEN);
    var s = await r.json();
    show(s.stage);
  } catch (e) {}
}
async function post(path, body) {
  document.getElementById('err').textContent = '';
  var r = await fetch(path + '?t=' + TOKEN, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
  if (!r.ok) {
    var j = await r.json().catch(function() { return {}; });
    document.getElementById('err').textContent = j.error || ('HTTP ' + r.status);
    return null;
  }
  return r.json();
}
async function myIP() {
  try {
    var r = await fetch('https://api.ipify.org?format=json');
    var j = await r.json();
    return j.ip;
  } catch (e) { return ''; }
}
async function doPair() {
  var body = {port: document.getElementById('pport').value.trim(), code: document.getElementById('pcode').value.trim()};
  if (WAN) { body.ip = await myIP(); }
  var res = await post('/api/pair', body);
  if (res) { poll(); }
}
async function doConnect() {
  var body = {port: document.getElementById('cport').value.trim()};
  var res = await post('/api/connect', body);
  if (res) { poll(); }
}
poll();
setInterval(poll, 3000);
</script></body></html>`
