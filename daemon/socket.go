package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"time"
)

// The socket API is deliberately tiny: one JSON object per line in, one
// JSON object per line out, over a Unix socket. It exists so a future
// GUI/status-bar integration (or just `daemon status` itself) can ask a
// running daemon "what's your state" without parsing log files or
// shelling out to `isolator` redundantly.
type socketRequest struct {
	Action string `json:"action"` // "status" | "trigger"
	Task   string `json:"task,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type socketResponse struct {
	OK     bool           `json:"ok"`
	Error  string         `json:"error,omitempty"`
	Status *statusPayload `json:"status,omitempty"`
}

type statusPayload struct {
	Uptime          string            `json:"uptime"`
	LastRun         map[string]string `json:"last_run"`
	UpdateEvery     string            `json:"update_interval"`
	AutoremoveEvery string            `json:"autoremove_interval"`
	CleanEvery      string            `json:"clean_interval"`
}

// ServeSocket listens on cfg.SocketPath until stopCh is closed. Any
// pre-existing socket file at that path is removed first (a leftover from
// an unclean shutdown — Unix sockets don't clean themselves up).
func ServeSocket(cfg DaemonConfig, sched *Scheduler, startedAt time.Time, log *Logger, stopCh <-chan struct{}) error {
	os.Remove(cfg.SocketPath)
	l, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return err
	}
	defer l.Close()
	defer os.Remove(cfg.SocketPath)

	go func() {
		<-stopCh
		l.Close()
	}()

	log.Printf("socket API listening on %s", cfg.SocketPath)
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-stopCh:
				return nil
			default:
				log.Printf("socket accept error: %v", err)
				continue
			}
		}
		go handleConn(conn, sched, startedAt, log)
	}
}

func handleConn(conn net.Conn, sched *Scheduler, startedAt time.Time, log *Logger) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)

	for scanner.Scan() {
		var req socketRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			enc.Encode(socketResponse{OK: false, Error: "invalid JSON: " + err.Error()})
			continue
		}

		switch req.Action {
		case "status":
			sched.mu.Lock()
			lastRun := map[string]string{}
			for k, v := range sched.lastRun {
				lastRun[k] = v.Format(time.RFC3339)
			}
			sched.mu.Unlock()

			enc.Encode(socketResponse{OK: true, Status: &statusPayload{
				Uptime:          time.Since(startedAt).Round(time.Second).String(),
				LastRun:         lastRun,
				UpdateEvery:     sched.cfg.UpdateInterval.String(),
				AutoremoveEvery: sched.cfg.AutoremoveInterval.String(),
				CleanEvery:      sched.cfg.CleanInterval.String(),
			}})

		case "trigger":
			if err := TriggerNow(req.Task, req.DryRun); err != nil {
				enc.Encode(socketResponse{OK: false, Error: err.Error()})
				continue
			}
			enc.Encode(socketResponse{OK: true})

		default:
			enc.Encode(socketResponse{OK: false, Error: "unknown action " + req.Action})
		}
	}
}

// queryDaemon is the client side used by `daemon status`/`daemon trigger`
// when talking to an already-running daemon over its socket.
func queryDaemon(socketPath string, req socketRequest) (*socketResponse, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	var resp socketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
