package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runRunCmd(os.Args[2:])
	case "status":
		runStatusCmd(os.Args[2:])
	case "trigger":
		runTriggerCmd(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// checkIsolator mirrors Builder's own dependency check — the daemon does
// nothing but schedule calls to the real `isolator` binary, so without it
// on PATH there's nothing for the daemon to actually do.
func checkIsolator() error {
	if _, err := exec.LookPath("isolator"); err != nil {
		return fmt.Errorf("isolator not found on PATH — isolator-daemon requires an Isolator install (https://github.com/HackerOS-Linux-System/Isolator)")
	}
	return nil
}

func runRunCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.Parse(args)

	if err := checkIsolator(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	configPath := ""
	if fs.NArg() >= 1 {
		configPath = fs.Arg(0)
	}
	cfg, err := LoadDaemonConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	logger, err := NewLogger(cfg.LogPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer logger.Close()

	startedAt := time.Now()
	sched := NewScheduler(cfg, logger)

	stopCh := make(chan struct{})
	go func() {
		if err := ServeSocket(cfg, sched, startedAt, logger, stopCh); err != nil {
			logger.Printf("socket server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stopCh)
		sched.Stop()
	}()

	sched.Run()
}

func runStatusCmd(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	socketPath := fs.String("socket", DefaultDaemonConfig().SocketPath, "path to the daemon's Unix socket")
	fs.Parse(args)

	resp, err := queryDaemon(*socketPath, socketRequest{Action: "status"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not reach daemon at %s: %v\n", *socketPath, err)
		fmt.Fprintln(os.Stderr, "(is it running? try: isolator-daemon run &)")
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, "daemon returned an error: "+resp.Error)
		os.Exit(1)
	}
	s := resp.Status
	fmt.Printf("uptime:              %s\n", s.Uptime)
	fmt.Printf("update interval:     %s\n", s.UpdateEvery)
	fmt.Printf("autoremove interval: %s\n", s.AutoremoveEvery)
	fmt.Printf("clean interval:      %s\n", s.CleanEvery)
	fmt.Println("last run:")
	if len(s.LastRun) == 0 {
		fmt.Println("  (nothing has run yet)")
	}
	for task, ts := range s.LastRun {
		fmt.Printf("  %-12s %s\n", task, ts)
	}
}

func runTriggerCmd(args []string) {
	fs := flag.NewFlagSet("trigger", flag.ExitOnError)
	socketPath := fs.String("socket", "", "talk to a running daemon over its socket instead of running directly")
	dryRun := fs.Bool("dry-run", false, "pass --dry-run through to the underlying isolator command")
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: daemon trigger [--dry-run] [--socket PATH] <update|autoremove|clean|snapshot|rollback>")
		os.Exit(1)
	}
	task := fs.Arg(0)

	if *socketPath != "" {
		resp, err := queryDaemon(*socketPath, socketRequest{Action: "trigger", Task: task, DryRun: *dryRun})
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not reach daemon at %s: %v\n", *socketPath, err)
			os.Exit(1)
		}
		if !resp.OK {
			fmt.Fprintln(os.Stderr, "trigger failed: "+resp.Error)
			os.Exit(1)
		}
		fmt.Println("triggered: " + task)
		return
	}

	if err := checkIsolator(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := TriggerNow(task, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const usageText = `isolator-daemon — unattended scheduling for Isolator's own maintenance
commands (update / autoremove / clean / snapshot). A separate tool, its
own module, requires isolator on PATH — it has no container-management
logic of its own, it's a clock that calls the real isolator commands.

Usage:
  isolator-daemon run [daemon.hk]        run in the foreground (systemd
                                          manages backgrounding — see
                                          the example unit below)
  isolator-daemon status [--socket PATH]
  isolator-daemon trigger [--dry-run] [--socket PATH] <task>
                                          task = update | autoremove |
                                          clean | snapshot | rollback

daemon.hk (all keys optional — every one has a sane default):

  [schedule]
  -> update_interval        => 24h
  -> autoremove_interval    => 168h
  -> clean_interval         => 168h
  -> snapshot_before_update => true

  [socket]
  -> path => /run/isolator-daemon.sock

  [log]
  -> path => /var/log/isolator-daemon.log

Example systemd unit (pairs with Builder's generated
isolator-first-boot.service — both assume Isolator itself is already set
up):

  [Unit]
  Description=Isolator unattended maintenance daemon
  After=network-online.target

  [Service]
  ExecStart=/usr/local/bin/isolator-daemon run /etc/isolator/daemon.hk
  Restart=on-failure

  [Install]
  WantedBy=multi-user.target`

func printUsage() {
	fmt.Println(usageText)
}
