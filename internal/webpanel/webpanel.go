// Package webpanel serves a token-gated browser control panel for a
// connected phone: live screenshot, tap-to-touch, text input and
// hardware keys from any browser — optionally exposed over the
// internet through a pinggy tunnel.
package webpanel

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/dev-roba/adbgo/internal/adb"
	"github.com/dev-roba/adbgo/internal/qr"
	"github.com/dev-roba/adbgo/internal/share"
	"github.com/dev-roba/adbgo/internal/ui"
)

var keyCodes = map[string]string{}

func init() {
	for _, kv := range []string{
		"back|4",
		"home|3",
		"recents|187",
		"power|26",
		"volup|24",
		"voldown|25",
		"wake|224",
		"menu|82",
		"enter|66",
		"del|67",
	} {
		p := strings.SplitN(kv, "|", 2)
		keyCodes[p[0]] = p[1]
	}
}

// panel is the per-session web control panel server.
type panel struct {
	serial string
	token  string
	shotMu sync.Mutex
}

func (p *panel) ok(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("t") != p.token {
		http.Error(w, "invalid token", http.StatusForbidden)
		return false
	}
	return true
}

func (p *panel) handlePage(w http.ResponseWriter, r *http.Request) {
	html := strings.ReplaceAll(pageHTML, "__TOKEN__", p.token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// screenshotBytes pulls one fresh screenshot as PNG bytes.
func (p *panel) screenshotBytes() ([]byte, error) {
	p.shotMu.Lock()
	defer p.shotMu.Unlock()
	file, err := adb.Screenshot(p.serial)
	if err != nil {
		return nil, err
	}
	defer os.Remove(file)
	return os.ReadFile(file)
}

func (p *panel) handleShot(w http.ResponseWriter, r *http.Request) {
	if !p.ok(w, r) {
		return
	}
	data, err := p.screenshotBytes()
	if err != nil {
		http.Error(w, "screenshot failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (p *panel) handleTap(w http.ResponseWriter, r *http.Request) {
	if !p.ok(w, r) {
		return
	}
	x, xerr := strconv.Atoi(r.URL.Query().Get("x"))
	y, yerr := strconv.Atoi(r.URL.Query().Get("y"))
	if xerr != nil || yerr != nil {
		http.Error(w, "bad coords", http.StatusBadRequest)
		return
	}
	if _, err := adb.Run(p.serial, "shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, "{\"ok\":true}")
}

func (p *panel) handleText(w http.ResponseWriter, r *http.Request) {
	if !p.ok(w, r) {
		return
	}
	text := r.URL.Query().Get("q")
	if text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	if err := adb.SendInputText(p.serial, text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, "{\"ok\":true}")
}

func (p *panel) handleKey(w http.ResponseWriter, r *http.Request) {
	if !p.ok(w, r) {
		return
	}
	code, valid := keyCodes[r.URL.Query().Get("k")]
	if !valid {
		http.Error(w, "unknown key", http.StatusBadRequest)
		return
	}
	if _, err := adb.Run(p.serial, "shell", "input", "keyevent", code); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, "{\"ok\":true}")
}

// Run starts the panel in mode "lan" or "wan" and blocks until the user
// presses Enter in the terminal.
func Run(serial, mode string) {
	if mode != "wan" {
		mode = "lan"
	}
	if !adb.IsConnected(serial) {
		ui.PrintError("Device " + serial + " is not online.")
		return
	}
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		ui.PrintError("Could not start server: " + err.Error())
		return
	}
	port := ln.Addr().(*net.TCPAddr).Port
	token := share.NewToken()

	url := fmt.Sprintf("http://%s:%d/p/%s", share.LanIP(), port, token)
	if mode == "wan" {
		ui.PrintInfo("Opening internet tunnel via pinggy.io...")
		publicURL, sshCmd, terr := share.StartTunnel(port)
		if terr != nil {
			_ = ln.Close()
			ui.PrintError(terr.Error())
			return
		}
		url = strings.TrimSuffix(publicURL, "/") + "/p/" + token
		defer func() {
			if sshCmd != nil && sshCmd.Process != nil {
				_ = sshCmd.Process.Kill()
			}
		}()
	}

	p := &panel{serial: serial, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/p/"+token+"/", p.handlePage)
	mux.HandleFunc("/p/"+token, p.handlePage)
	mux.HandleFunc("/api/shot", p.handleShot)
	mux.HandleFunc("/api/tap", p.handleTap)
	mux.HandleFunc("/api/text", p.handleText)
	mux.HandleFunc("/api/key", p.handleKey)
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	fmt.Println()
	fmt.Println(ui.TitleStyle.Render("🌐 Web panel (" + strings.ToUpper(mode) + ")"))
	fmt.Println()
	fmt.Println("  Open on any device:  " + url)
	fmt.Println()
	if mat, qerr := qr.Encode(url); qerr == nil {
		fmt.Println(qr.RenderANSI(mat))
	} else {
		ui.PrintWarning("Could not render QR: " + qerr.Error())
	}
	fmt.Println(ui.DimStyle.Render("Live screenshot · click to tap · type text · press keys"))
	fmt.Println(ui.DimStyle.Render("Press Enter in this terminal to stop the panel."))
	ui.ReadLine()
	_ = srv.Close()
	ui.PrintInfo("Web panel stopped.")
}

// pageHTML is the browser panel.
const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>adbgo — phone panel</title>
<style>
body{background:#0f1117;color:#e6e6e6;font-family:system-ui,sans-serif;margin:0;padding:16px;display:flex;flex-direction:column;align-items:center;gap:12px}
img{max-width:100%;border-radius:12px;border:1px solid #3a3f52;user-select:none;-webkit-user-select:none}
.row{display:flex;gap:8px;flex-wrap:wrap;justify-content:center}
button{background:#2a2f3f;color:#fff;border:0;border-radius:8px;padding:10px 14px;font-size:.95rem}
button:active{background:#4f8cff}
input{padding:10px;border-radius:8px;border:1px solid #3a3f52;background:#0f1117;color:#fff;font-size:1rem;width:240px}
small{color:#a8b0c0}
</style></head><body>
<h3>📱 adbgo remote</h3>
<img id="scr" draggable="false">
<div class="row">
<button onclick="key('back')">◀ Back</button>
<button onclick="key('home')">⌂ Home</button>
<button onclick="key('recents')">▤ Recents</button>
<button onclick="key('power')">⏻ Power</button>
<button onclick="key('volup')">Vol+</button>
<button onclick="key('voldown')">Vol−</button>
<button onclick="key('wake')">☀ Wake</button>
</div>
<div class="row">
<input id="txt" placeholder="type text...">
<button onclick="sendText()">Send</button>
<button onclick="key('enter')">Enter</button>
<button onclick="key('del')">Del</button>
</div>
<small>click the screenshot to tap · refresh <span id="hz">1s</span> · <button onclick="cycle()">change</button></small>
<script>
var T="__TOKEN__";
var hzList=[500,1000,2000,0];
var hi=1;
var hz=hzList[hi];
var img=document.getElementById('scr');
function refresh(){img.src='/api/shot?t='+T+'&r='+Math.random();}
img.onload=function(){if(hz>0){setTimeout(refresh,hz);}}
img.onerror=function(){setTimeout(refresh,3000);}
img.onclick=function(e){
  var r=img.getBoundingClientRect();
  var x=Math.round((e.clientX-r.left)/r.width*img.naturalWidth);
  var y=Math.round((e.clientY-r.top)/r.height*img.naturalHeight);
  fetch('/api/tap?t='+T+'&x='+x+'&y='+y).then(refresh);
}
function key(k){fetch('/api/key?t='+T+'&k='+k).then(refresh);}
function sendText(){
  var v=document.getElementById('txt').value;
  if(!v){return;}
  fetch('/api/text?t='+T+'&q='+encodeURIComponent(v)).then(refresh);
}
function cycle(){
  hi=(hi+1)%hzList.length;
  hz=hzList[hi];
  document.getElementById('hz').textContent=hz>0?hz+'ms':'off';
  if(hz>0){refresh();}
}
refresh();
</script></body></html>`
