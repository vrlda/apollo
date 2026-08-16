package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// The whole app is behind BasicAuth so we trust same-origin connections
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// TerminalHandler: GET /ws/terminal
// Upgrades to a WebSocket connection, spawns a bash PTY, and proxies data bidirectionally.
func TerminalHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	// Determine starting directory
	startDir := os.Getenv("HOME")
	if startDir == "" {
		startDir = "/"
	}
	if projectPath := r.URL.Query().Get("path"); projectPath != "" {
		if _, err := os.Stat(projectPath); err == nil {
			startDir = projectPath
		}
	}

	// Spawn a shell inside a PTY
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Dir = startDir

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Println("PTY start failed:", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to start terminal: "+err.Error()))
		return
	}
	defer func() {
		ptmx.Close()
		cmd.Process.Kill()
	}()

	// PTY → WebSocket: forward all terminal output to the browser
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if err2 := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err2 != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY: forward all keyboard input from browser to the shell
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage || msgType == websocket.BinaryMessage {
			// Check for a resize message: JSON {"type":"resize","cols":N,"rows":N}
			if len(data) > 0 && data[0] == '{' {
				var msg struct {
					Type string `json:"type"`
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if err := json.Unmarshal(data, &msg); err == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
					continue
				}
			}
			ptmx.Write(data)
		}
	}
}
