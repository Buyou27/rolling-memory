// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestShutdownStaleUnixDaemonAuthenticatesVault(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	if !unixDaemonPeerVerificationAvailable() {
		t.Skip("Unix daemon peer verification unavailable")
	}
	memory := newMemory(t)
	configID, err := unixDaemonConfigID(memory.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "match", true: "mismatch"}[mismatch], func(t *testing.T) {
			sock := testUnixDaemonLeasePath(t)
			marker := filepath.Join(filepath.Dir(sock), "shutdown-called")
			db := memory.Config.DB
			if mismatch {
				db += "-other"
			}
			script := `import http.server,json,os,socketserver,sys
sock, db, cid, marker = sys.argv[1:5]
class Handler(http.server.BaseHTTPRequestHandler):
 protocol_version = 'HTTP/1.1'
 def do_GET(self):
  data=json.dumps({'ok':True,'pid':os.getpid(),'db':db,'identity_version':1,'mode':'local','pulse_mode':'local_plain','config_id':cid}).encode()
  self.send_response(200); self.send_header('Content-Length',str(len(data))); self.end_headers(); self.wfile.write(data)
 def do_POST(self):
  open(marker,'w').close()
  try: os.unlink(sock)
  except OSError: pass
  self.send_response(200); self.send_header('Content-Length','0'); self.end_headers()
 def log_message(self,*args): pass
socketserver.UnixStreamServer(sock,Handler).serve_forever()`
			cmd := exec.Command(python, "-I", "-c", script, sock, db, configID, marker)
			cmd.Env = []string{"HOME=" + filepath.Dir(sock), "PATH=/usr/bin:/bin"}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}()
			deadline := time.Now().Add(2 * time.Second)
			for {
				if _, err := os.Lstat(sock); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fake daemon did not bind")
				}
				time.Sleep(10 * time.Millisecond)
			}
			info, err := os.Lstat(sock)
			if err != nil {
				t.Fatal(err)
			}
			shutdownErr := shutdownStaleUnixDaemon(sock, info, memory.Config)
			if socketInfo, socketErr := os.Lstat(sock); socketErr == nil && mismatch {
				t.Logf("mismatched socket still present: %v", socketInfo.Mode())
			}
			_, markerErr := os.Stat(marker)
			if mismatch {
				if shutdownErr == nil {
					t.Fatalf("mismatched vault was shut down")
				}
				if !os.IsNotExist(markerErr) {
					t.Fatalf("mismatched vault received a shutdown request")
				}
				return
			}
			if shutdownErr != nil {
				t.Fatalf("matching vault shutdown: %v", shutdownErr)
			}
			if os.IsNotExist(markerErr) {
				t.Fatalf("shutdown request was not written")
			}
			if _, err := os.Lstat(sock); !os.IsNotExist(err) {
				t.Fatalf("socket still present after shutdown: %v", err)
			}
		})
	}
}
