package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // JWT auth is enforced below
}

// resizeQueue feeds terminal resize events into the K8s SPDY executor.
type resizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func (q *resizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.ch
	if !ok {
		return nil
	}
	return &size
}

// NodeTerminal upgrades the HTTP connection to WebSocket and streams an
// interactive root shell on the target worker node via a K8s privileged pod.
// The JWT must be passed as the `token` query parameter.
//
// Protocol:
//   - Client → Server text message: JSON {"type":"resize","cols":N,"rows":N}
//   - Client → Server binary message: raw stdin bytes
//   - Server → Client binary message: raw stdout/stderr bytes
func (h *Handler) NodeTerminal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ── Auth: validate JWT from query param (browser WS can't set headers) ──
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// ── Load node ────────────────────────────────────────────────────────────
	orgIDStr := chi.URLParam(r, "orgId")
	nodeIDStr := chi.URLParam(r, "nodeId")

	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		http.Error(w, "invalid orgId", http.StatusBadRequest)
		return
	}
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		http.Error(w, "invalid nodeId", http.StatusBadRequest)
		return
	}

	node, err := h.svc.Nodes.Get(ctx, nodeID)
	if err != nil || node.OrganizationID != orgID {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	// ── Block gateway node ────────────────────────────────────────────────────
	if node.K3sRole == "server" {
		http.Error(w, "terminal not available on gateway nodes", http.StatusForbidden)
		return
	}

	// ── K8s availability check ────────────────────────────────────────────────
	if h.svc.K8s == nil {
		http.Error(w, "kubernetes not available", http.StatusServiceUnavailable)
		return
	}

	// ── Resolve K8s node name ─────────────────────────────────────────────────
	k8sNodeName, err := appk8s.ResolveK8sNodeName(ctx, h.svc.K8s, node.TailscaleIP, node.Name)
	if err != nil {
		http.Error(w, fmt.Sprintf("node not in k8s cluster: %v", err), http.StatusUnprocessableEntity)
		return
	}

	// ── Create shell pod ──────────────────────────────────────────────────────
	podName := fmt.Sprintf("meshploy-shell-%s", strings.ReplaceAll(nodeID.String()[:8], "-", ""))
	// Best-effort delete any leftover pod from a previous session.
	appk8s.DeleteShellPod(context.Background(), h.svc.K8s, podName)

	startCtx, cancel := context.WithTimeout(ctx, 30_000_000_000) // 30s
	defer cancel()

	if _, err := appk8s.CreateShellPod(startCtx, h.svc.K8s, k8sNodeName, podName); err != nil {
		http.Error(w, fmt.Sprintf("create shell pod: %v", err), http.StatusInternalServerError)
		return
	}
	defer appk8s.DeleteShellPod(context.Background(), h.svc.K8s, podName)

	if err := appk8s.WaitForPodRunning(startCtx, h.svc.K8s, podName); err != nil {
		http.Error(w, fmt.Sprintf("pod not ready: %v", err), http.StatusInternalServerError)
		return
	}

	// ── Upgrade WebSocket ─────────────────────────────────────────────────────
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("terminal: ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	// ── Build K8s exec request ────────────────────────────────────────────────
	sizeQ := &resizeQueue{ch: make(chan remotecommand.TerminalSize, 4)}
	defer close(sizeQ.ch)

	req := h.svc.K8s.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace("kube-system").
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "shell",
			// chroot into host filesystem → real root shell on the node, starting at /root
			Command: []string{"chroot", "/host", "bash", "-c", "cd /root && exec bash -l"},
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}, scheme.ParameterCodec)

	restCfg := h.svc.K8sRestConfig
	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		return
	}

	// ── Bridge WebSocket ↔ exec ───────────────────────────────────────────────
	// execCtx is cancelled the moment the WebSocket reader exits (client closed
	// tab / browser). This unblocks StreamWithContext so the defer DeleteShellPod
	// fires even though gorilla hijacks the connection and r.Context() is never
	// cancelled by the HTTP server.
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	var wsMu sync.Mutex
	writeWS := func(data []byte) {
		wsMu.Lock()
		defer wsMu.Unlock()
		conn.WriteMessage(websocket.BinaryMessage, data)
	}

	// WebSocket → stdin (+ resize handling)
	go func() {
		defer stdinW.Close()
		defer execCancel() // unblock StreamWithContext when WS closes
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var ev struct {
					Type string `json:"type"`
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg, &ev) == nil && ev.Type == "resize" {
					select {
					case sizeQ.ch <- remotecommand.TerminalSize{Width: ev.Cols, Height: ev.Rows}:
					default:
					}
				}
			} else {
				stdinW.Write(msg)
			}
		}
	}()

	// stdout/stderr → WebSocket
	stdoutR, stdoutW := io.Pipe()
	go func() {
		defer stdoutR.Close()
		buf := make([]byte, 4096)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				writeWS(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	err = executor.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdin:             stdinR,
		Stdout:            stdoutW,
		Stderr:            stdoutW, // merge stderr into stdout for the terminal
		Tty:               true,
		TerminalSizeQueue: sizeQ,
	})
	if err != nil {
		log.Printf("terminal: stream ended: %v", err)
	}
}

// ServiceTerminal upgrades the HTTP connection to WebSocket and streams an
// interactive shell inside an existing service pod via K8s exec.
// The JWT must be passed as the `token` query parameter.
//
// Protocol is identical to NodeTerminal (resize JSON + binary stdin/stdout).
func (h *Handler) ServiceTerminal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	orgIDStr := chi.URLParam(r, "orgId")
	serviceIDStr := chi.URLParam(r, "serviceId")
	podName := chi.URLParam(r, "podName")

	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		http.Error(w, "invalid orgId", http.StatusBadRequest)
		return
	}
	serviceID, err := uuid.Parse(serviceIDStr)
	if err != nil {
		http.Error(w, "invalid serviceId", http.StatusBadRequest)
		return
	}
	_ = orgID // future: verify org membership

	if h.svc.K8s == nil {
		http.Error(w, "kubernetes not available", http.StatusServiceUnavailable)
		return
	}

	namespace, containerName, err := h.svc.Workloads.GetK8sInfo(ctx, serviceID)
	if err != nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("service-terminal: ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	sizeQ := &resizeQueue{ch: make(chan remotecommand.TerminalSize, 4)}
	defer close(sizeQ.ch)

	req := h.svc.K8s.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   []string{"sh", "-c", "export TERM=xterm-256color; exec bash -l 2>/dev/null || exec sh -l"},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	restCfg := h.svc.K8sRestConfig
	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		return
	}

	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()

	var wsMu sync.Mutex
	writeWS := func(data []byte) {
		wsMu.Lock()
		defer wsMu.Unlock()
		conn.WriteMessage(websocket.BinaryMessage, data)
	}

	go func() {
		defer stdinW.Close()
		defer execCancel()
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var ev struct {
					Type string `json:"type"`
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg, &ev) == nil && ev.Type == "resize" {
					select {
					case sizeQ.ch <- remotecommand.TerminalSize{Width: ev.Cols, Height: ev.Rows}:
					default:
					}
				}
			} else {
				stdinW.Write(msg)
			}
		}
	}()

	stdoutR, stdoutW := io.Pipe()
	go func() {
		defer stdoutR.Close()
		buf := make([]byte, 4096)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				writeWS(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	err = executor.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdin:             stdinR,
		Stdout:            stdoutW,
		Stderr:            stdoutW,
		Tty:               true,
		TerminalSizeQueue: sizeQ,
	})
	if err != nil {
		log.Printf("service-terminal: stream ended: %v", err)
	}
}
