package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"golang.org/x/term"

	"gravlink/internal/agy"
	"gravlink/internal/config"
	"gravlink/internal/security"
	"gravlink/internal/server"
	"gravlink/internal/workspace"
	"gravlink/web"
)

const version = "0.1.0-m1"

func main() {
	if len(os.Args) < 2 {
		runStart(os.Args[1:])
		return
	}

	sub := os.Args[1]
	switch sub {
	case "start":
		runStart(os.Args[2:])
	case "setup":
		runSetup(os.Args[2:])
	case "doctor":
		runDoctor(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("gravlink %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		if strings.HasPrefix(sub, "-") {
			runStart(os.Args[1:])
			return
		}
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", sub)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`Gravlink — LAN-only mobile companion for Antigravity IDE

Usage:
  gravlink start [flags]   Start the LAN web server (default)
  gravlink setup [flags]   Set or update the host login password
  gravlink doctor [flags]  Verify Antigravity IDE integration contracts
  gravlink version         Print version`)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	port := fs.Int("port", 0, "TCP port to listen on (default 8443)")
	ipFlag := fs.String("ip", "", "Override primary LAN IPv4 address for QR code (auto-detected by default)")
	wsPath := fs.String("workspace", "", "Workspace directory to serve read-only (default current dir)")
	dataDir := fs.String("data-dir", "", "Gravlink data directory (default ~/.gravlink)")
	useHTTP := fs.Bool("http", false, "Serve plain HTTP instead of local HTTPS (not recommended for mobile PWA)")
	allRFC1918 := fs.Bool("allow-all-rfc1918", false, "Allow any RFC1918 private IP rather than only active LAN interface subnets")
	passwordFlag := fs.String("password", "", "Set password non-interactively (prefer interactive prompt)")
	_ = fs.Parse(args)

	cfg, err := config.LoadOrInit(*dataDir)
	must(err)

	if *port > 0 {
		cfg.Port = *port
	}
	if *wsPath != "" {
		cfg.Workspace = *wsPath
	} else if cfg.Workspace == "" {
		cwd, err := os.Getwd()
		must(err)
		cfg.Workspace = cwd
	}
	cfg.UseTLS = !*useHTTP
	cfg.StrictSubnet = !*allRFC1918

	if *passwordFlag != "" {
		hash, err := security.HashPassword(*passwordFlag)
		must(err)
		cfg.PasswordHash = hash
		must(config.Save(cfg))
	} else if cfg.PasswordHash == "" {
		fmt.Println("No Gravlink password configured yet. Please set a host password (min 8 chars):")
		pw, err := promptPassword()
		must(err)
		hash, err := security.HashPassword(pw)
		must(err)
		cfg.PasswordHash = hash
		must(config.Save(cfg))
		fmt.Println("Password saved (Argon2id).")
	}

	audit, err := security.NewAuditLogger(cfg.DataDir)
	must(err)
	defer func() { _ = audit.Close() }()

	exp, err := workspace.NewExplorer(cfg.Workspace)
	must(err)

	localSubnets, lanIPs := security.LocalInterfaceSubnets()
	if *ipFlag != "" {
		if parsed := net.ParseIP(strings.TrimSpace(*ipFlag)); parsed != nil {
			lanIPs = append([]net.IP{parsed}, lanIPs...)
		}
	}
	var strictSubnets []*net.IPNet
	if cfg.StrictSubnet {
		strictSubnets = localSubnets
	}

	var tlsCert tls.Certificate
	var caPEM []byte
	if cfg.UseTLS {
		tlsCert, caPEM, err = security.EnsureLocalTLS(cfg.DataDir, lanIPs)
		must(err)
	}

	sm, err := security.NewSessionManager(
		cfg.SessionSecret,
		time.Duration(cfg.SessionTTLHours)*time.Hour,
		cfg.UseTLS,
	)
	must(err)

	pairToken, err := sm.CreatePairingToken(5 * time.Minute)
	must(err)

	staticFS, err := web.DistFS()
	must(err)

	handler := server.NewRouter(server.Dependencies{
		Config:        cfg,
		Sessions:      sm,
		RateLimiter:   security.NewAuthRateLimiter(5, 15*time.Minute),
		Audit:         audit,
		Explorer:      exp,
		StrictSubnets: strictSubnets,
		CACertPEM:     caPEM,
		StaticFS:      staticFS,
	})

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	scheme := "https"
	if !cfg.UseTLS {
		scheme = "http"
	}

	fmt.Printf("\nGravlink %s listening on %s://0.0.0.0:%d (LAN ONLY)\n", version, scheme, cfg.Port)
	fmt.Printf("Workspace (read-only): %s\n", exp.Root())
	fmt.Printf("Audit log: %s\n", filepath.Join(cfg.DataDir, "audit.log"))

	fwOK, fwDetail := security.CheckPrivateFirewallStatus()
	if !fwOK {
		fmt.Printf("\n[Firewall Notice] %s\n", fwDetail)
		fmt.Printf("If your phone cannot connect over Wi-Fi, run in an Administrator PowerShell:\n  %s\n", security.PrivateFirewallCommand(cfg.Port))
	}

	printPairBanner := func(tok string) {
		if len(lanIPs) > 0 {
			primaryURL := fmt.Sprintf("%s://%s:%d", scheme, lanIPs[0].String(), cfg.Port)
			pairURL := fmt.Sprintf("%s/?pair=%s", primaryURL, tok)
			fmt.Printf("\nLaptop test URL (password login): %s://127.0.0.1:%d\n", scheme, cfg.Port)
			fmt.Printf("Phone LAN URL (same Wi-Fi):       %s\n", primaryURL)
			if len(lanIPs) > 1 {
				var others []string
				for _, ip := range lanIPs[1:] {
					others = append(others, ip.String())
				}
				fmt.Printf("Other local interface IPs:        %s\n", strings.Join(others, ", "))
			}
			fmt.Printf("\nScan this one-time QR code on your phone (valid 5 min; do NOT open pair URL on laptop):\n")
			qrterminal.GenerateHalfBlock(pairURL, qrterminal.L, os.Stdout)
			fmt.Printf("One-time pair URL: %s\n", pairURL)
			fmt.Printf("Press [Enter] anytime to generate a fresh QR code.\n\n")
		} else {
			fmt.Printf("\nLocal URL: %s://127.0.0.1:%d/?pair=%s\n\n", scheme, cfg.Port, tok)
		}
	}
	printPairBanner(pairToken)

	// Allow generating a fresh one-time QR pairing code anytime by pressing Enter.
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			newTok, err := sm.CreatePairingToken(5 * time.Minute)
			if err == nil {
				printPairBanner(newTok)
			}
		}
	}()

	audit.Log(security.AuditEvent{
		Event:    "server_start",
		ClientIP: "127.0.0.1",
		Outcome:  "success",
		Detail:   fmt.Sprintf("port=%d tls=%v strict_subnet=%v", cfg.Port, cfg.UseTLS, cfg.StrictSubnet),
	})

	ln, err := net.Listen("tcp", addr)
	must(err)

	errCh := make(chan error, 1)
	go func() {
		if cfg.UseTLS {
			srv.TLSConfig = &tls.Config{
				Certificates: []tls.Certificate{tlsCert},
				MinVersion:   tls.VersionTLS12,
			}
			errCh <- srv.ServeTLS(ln, "", "")
		} else {
			errCh <- srv.Serve(ln)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigCh:
		fmt.Println("\nShutting down Gravlink...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			must(err)
		}
	}
}

func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "Gravlink data directory (default ~/.gravlink)")
	passwordFlag := fs.String("password", "", "Password to set")
	_ = fs.Parse(args)

	cfg, err := config.LoadOrInit(*dataDir)
	must(err)

	pw := *passwordFlag
	if pw == "" {
		fmt.Println("Enter new Gravlink password (min 8 chars):")
		pw, err = promptPassword()
		must(err)
	}
	hash, err := security.HashPassword(pw)
	must(err)
	cfg.PasswordHash = hash
	must(config.Save(cfg))
	fmt.Println("Password updated successfully.")
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	wsPath := fs.String("workspace", "", "Workspace directory to check")
	dataDir := fs.String("data-dir", "", "Gravlink data directory")
	_ = fs.Parse(args)

	cfg, err := config.LoadOrInit(*dataDir)
	must(err)

	ws := *wsPath
	if ws == "" {
		ws = cfg.Workspace
	}
	if ws == "" {
		ws, _ = os.Getwd()
	}

	checks := agy.RunDoctor(context.Background(), cfg.AntigravityDir, ws)
	failed := false
	for _, c := range checks {
		fmt.Printf("[%s] %-22s %s\n", c.Status, c.Name, c.Detail)
		if c.Status == "FAIL" {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func promptPassword() (string, error) {
	fmt.Print("Password: ")
	b1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Confirm:  ")
	b2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(b1) != string(b2) {
		return "", fmt.Errorf("passwords do not match")
	}
	return string(b1), nil
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
