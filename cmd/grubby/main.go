package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"grubby/internal/config"
	"grubby/internal/eventbus"
	"grubby/internal/git"
	"grubby/internal/github"
	"grubby/internal/ingress"
	"grubby/internal/storage"
	"grubby/internal/ui"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	var (
		orgFlag         string
		workspaceFlag   string
		configFlag      string
		proxyFlag       string
		webhookPortFlag int
		noWebhookFlag   bool
		insecureFlag    bool
		versionFlag     bool
	)

	flag.StringVar(&orgFlag, "org", "", "Target GitHub organization or user")
	flag.StringVar(&workspaceFlag, "workspace", "", "Path to local workspace directory (defaults to ~/workspaces)")
	flag.StringVar(&configFlag, "config", "", "Path to Grubby config file (defaults to ~/.config/grubby/config.yaml)")
	flag.StringVar(&proxyFlag, "proxy", "", "HTTP, HTTPS, or SOCKS5 corporate proxy URL (e.g. http://proxy.corp.com:8080)")
	flag.IntVar(&webhookPortFlag, "webhook-port", 0, "Port for embedded webhook server (defaults to 8484)")
	flag.BoolVar(&noWebhookFlag, "no-webhook", false, "Disable embedded webhook server and rely on polling fallback")
	flag.BoolVar(&insecureFlag, "insecure", false, "Skip TLS certificate verification (useful for corporate SSL inspection proxies)")
	flag.BoolVar(&versionFlag, "version", false, "Print Grubby version and exit")
	flag.BoolVar(&versionFlag, "v", false, "Print Grubby version and exit (shorthand)")
	flag.Parse()

	if versionFlag {
		fmt.Printf("grubby %s (%s, %s)\n", Version, Commit, Date)
		return
	}

	// Verify configuration exists or --org was provided on first invocation
	if !config.Exists(configFlag) && orgFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: No configuration found. You must pass the --org parameter the first time you invoke grubby (e.g. grubby --org <your-org>).")
		os.Exit(1)
	}

	// Check authentication
	token, err := github.GetGHToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Grubby requires GitHub CLI authentication. Please run:")
		fmt.Fprintln(os.Stderr, "  gh auth login --scopes repo,read:org")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if workspaceFlag != "" {
		cfg.WorkspaceDir = workspaceFlag
	}

	// Resolve proxy settings: CLI flag takes precedence over config
	proxyURL := proxyFlag
	if proxyURL == "" {
		proxyURL = cfg.Proxy
	}
	insecure := insecureFlag || cfg.InsecureSkipVerify

	// Initialize GitHub client
	ghClient, err := github.NewClientWithOptions(github.ClientOptions{
		Token:              token,
		ProxyURL:           proxyURL,
		InsecureSkipVerify: insecure,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize GitHub client: %v\n", err)
		os.Exit(1)
	}

	// Resolve target organization / owner:
	// When invoked with --org, that org is set as the home org
	targetOrg := orgFlag
	if targetOrg != "" {
		_ = cfg.SetHomeOrg(targetOrg)
		cfg.DefaultOrg = targetOrg
		_ = cfg.Save()
	} else if cfg.GetHomeOrg() != "" {
		targetOrg = cfg.GetHomeOrg()
	} else if cfg.DefaultOrg != "" {
		targetOrg = cfg.DefaultOrg
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		currentUser, err := ghClient.GetAuthenticatedUser(ctx)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not determine GitHub user: %v\n", err)
			os.Exit(1)
		}
		targetOrg = currentUser
		cfg.DefaultOrg = targetOrg
		_ = cfg.SetHomeOrg(targetOrg)
		_ = cfg.Save()
	}

	// Do squad lookup - finding none, create default squad and add that org as one of the squad orgs
	if len(cfg.Squads) == 0 {
		homeOrg := cfg.GetHomeOrg()
		centralOwner, centralRepo := cfg.GetCentralTasksRepo(homeOrg)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		squadsFile, _, err := ghClient.FetchSquadsFile(ctx, centralOwner, centralRepo)
		cancel()
		if err == nil && squadsFile != nil && len(squadsFile.Squads) > 0 {
			for _, s := range squadsFile.Squads {
				_ = cfg.CreateSquad(s.Name, s.DisplayName, s.Members)
				if len(s.Organizations) > 0 {
					_ = cfg.SetSquadOrgs(s.Name, s.Organizations)
				}
				if len(s.Repos) > 0 {
					_ = cfg.SetSquadRepos(s.Name, s.Repos)
				}
			}
			if len(cfg.Squads) > 0 {
				_ = cfg.SetActiveSquad(cfg.Squads[0].Name)
			}
			_ = cfg.Save()
		}

		if len(cfg.Squads) == 0 {
			defaultSquadName := "default"
			_ = cfg.CreateSquad(defaultSquadName, "Default Squad", []string{})
			_ = cfg.SetSquadOrgs(defaultSquadName, []string{homeOrg})
			_ = cfg.SetActiveSquad(defaultSquadName)
			_ = cfg.Save()
		}
	}

	// Initialize workspace manager
	gitMgr := git.NewWorkspaceManager(cfg.WorkspaceDir)

	// Apply Webhook configuration and flags
	if webhookPortFlag > 0 {
		cfg.Webhook.Port = webhookPortFlag
	}
	if noWebhookFlag {
		cfg.Webhook.Enabled = false
	} else if !cfg.Webhook.Enabled && cfg.Webhook.Port == 0 {
		// Enabled by default
		cfg.Webhook.Enabled = true
		cfg.Webhook.Port = 8484
	}

	// Initialize local SQLite database cache
	dbPath, _ := storage.DefaultDBPath()
	db, err := storage.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to open SQLite db: %v\n", err)
	} else {
		defer db.Close()
	}

	// Initialize event bus & ingress server if webhooks are enabled
	var bus *eventbus.Bus
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	if cfg.Webhook.Enabled && db != nil {
		bus = eventbus.NewBus(256)
		defer bus.Close()

		ingressCfg := ingress.Config{
			Enabled:          true,
			Port:             cfg.Webhook.Port,
			Path:             cfg.Webhook.Path,
			HomeOrg:          cfg.GetHomeOrg(),
			CentralTasksRepo: cfg.GetCentralTasksRepoFullName(targetOrg),
		}
		ingressServer := ingress.NewServer(ingressCfg, db, bus)
		go func() {
			_ = ingressServer.Start(appCtx)
		}()
	}

	// Launch TUI
	initialModel := ui.InitialModel(cfg, ghClient, gitMgr, targetOrg, db)
	if bus != nil {
		initialModel.SetEventBus(bus)
	}
	p := tea.NewProgram(initialModel, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Grubby exited with error: %v\n", err)
		os.Exit(1)
	}
}
