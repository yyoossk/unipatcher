// UniPatcher (万能ロムパッチャー) — いろいろな形式のロムパッチを当てたり、
// パッチを作ったりする単体 GUI。
//
// インストール不要。exe を起動するとローカルサーバーが立ち、
// WebView2 ウィンドウ (無ければ既定ブラウザ) に画面が出る。
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var buildStamp = "dev"

var exeDir string

func main() {
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	} else {
		exeDir = "."
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", pageHandler)
	mux.HandleFunc("/api/hash", hashHandler)
	mux.HandleFunc("/api/patchinfo", patchInfoHandler)
	mux.HandleFunc("/api/apply", applyHandler)
	mux.HandleFunc("/api/create", createHandler)
	mux.HandleFunc("/api/save", saveHandler)
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	})

	var ln net.Listener
	var err error
	for _, p := range []int{47710, 47711, 47712, 47713, 47714} {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			break
		}
	}
	if ln == nil {
		fmt.Fprintln(os.Stderr, "no free port in 47710-47714")
		os.Exit(1)
	}
	url := fmt.Sprintf("http://%s/?v=%d", ln.Addr().String(), time.Now().UnixNano())
	fmt.Println("UniPatcher:", url)
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	if !openEmbeddedUI(url) {
		openBrowser(url)
		select {}
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func pathOf(parts ...string) string {
	return filepath.Join(append([]string{exeDir}, parts...)...)
}
