// Package app provides the shared server wiring for Cogitator.
// Both the CLI binary and the desktop .app binary use this to
// construct and run the full server stack.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/api"
	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/budget"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/channel"
	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/connector"
	"github.com/cogitatorai/cogitator/server/internal/connector/browser"
	"github.com/cogitatorai/cogitator/server/internal/drain"
	"github.com/cogitatorai/cogitator/server/internal/heartbeat"
	"github.com/cogitatorai/cogitator/server/internal/mcp"
	"github.com/cogitatorai/cogitator/server/internal/metrics"
	"github.com/cogitatorai/cogitator/server/internal/social"
	"github.com/cogitatorai/cogitator/server/internal/secretstore"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/push"
	"github.com/cogitatorai/cogitator/server/internal/ollama"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/sandbox"
	"github.com/cogitatorai/cogitator/server/internal/security"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/skills"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"github.com/cogitatorai/cogitator/server/internal/tools"
	"github.com/cogitatorai/cogitator/server/internal/updater"
	"github.com/cogitatorai/cogitator/server/internal/user"
	"github.com/cogitatorai/cogitator/server/internal/version"
	"github.com/cogitatorai/cogitator/server/internal/worker"
	"github.com/cogitatorai/cogitator/server/internal/workspace"

	"gopkg.in/yaml.v3"
)

// Options configures how the server is constructed.
type Options struct {
	ConfigPath    string // Path to cogitator.yaml (empty = use workspace default)
	WorkspacePath string // Override workspace directory (empty = use config default)
	DashboardDir  string // Disk path to dashboard/dist/ (CLI mode)
	DashboardFS   fs.FS  // Embedded FS for the dashboard (desktop mode)
}

// Server holds every subsystem and the HTTP server, ready to start.
type Server struct {
	httpServer *http.Server
	addr       string
	wsRoot     string

	configStore *config.Store

	// Subsystems that need graceful shutdown.
	db            *database.DB
	eventBus      *bus.Bus
	enricher          *worker.Enricher
	profiler          *worker.Profiler
	consolidator      *worker.Consolidator
	reflector         *worker.Reflector
	reflectionTrigger *worker.ReflectionTrigger
	webChannel    *channel.WebChannel
	teleChannel   *channel.TelegramChannel
	taskScheduler *task.Scheduler
	updater       *updater.Updater
	mcpManager       *mcp.Manager
	browserConnector *browser.Connector
	pushDispatcher   *push.Dispatcher

	// SaaS-only subsystems (nil in CLI/desktop mode).
	heartbeat          *heartbeat.Heartbeat
	orchestratorURL    string
	tenantID           string
	saasInternalSecret string
}

// New wires every subsystem (config, workspace, database, bus, stores,
// agent, channels, router) and returns a ready-to-start Server.
func New(opts Options) (*Server, error) {
	config.LoadDotEnv()

	cfg := config.Default()
	cfgPath := opts.ConfigPath

	if cfgPath != "" {
		loaded, err := config.Load(cfgPath)
		if err != nil {
			return nil, fmt.Errorf("loading config: %w", err)
		}
		cfg = loaded
	}
	cfg.ApplyEnv()

	if opts.WorkspacePath != "" {
		cfg.Workspace.Path = opts.WorkspacePath
	}

	ws, err := workspace.Init(cfg.Workspace.Path)
	if err != nil {
		return nil, fmt.Errorf("initializing workspace: %w", err)
	}

	// Default config path to the workspace so dashboard settings persist across restarts.
	if cfgPath == "" {
		cfgPath = ws.ConfigPath()
		if data, err := os.ReadFile(cfgPath); err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("loading workspace config: %w", err)
			}
			cfg.ApplyEnv()
		}
	}

	db, err := database.Open(ws.DBPath())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.MigrateAudit(); err != nil {
		db.Close()
		return nil, fmt.Errorf("audit migration: %w", err)
	}

	// User store and admin bootstrap.
	userStore := user.NewStore(db)
	if adminUser, err := userStore.Bootstrap(
		os.Getenv("COGITATOR_ADMIN_USER"),
		os.Getenv("COGITATOR_ADMIN_PASSWORD"),
	); err != nil {
		return nil, fmt.Errorf("bootstrap admin: %w", err)
	} else if adminUser != nil {
		slog.Info("admin account created", "email", adminUser.Email)
	}

	// JWT service.
	jwtSecret := os.Getenv("COGITATOR_JWT_SECRET")
	if jwtSecret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("generate JWT secret: %w", err)
		}
		jwtSecret = hex.EncodeToString(b)
		slog.Warn("no COGITATOR_JWT_SECRET set, generated random secret (tokens will not survive restarts)")
	}
	jwtSvc := auth.NewJWTService(jwtSecret, 24*time.Hour, 30*24*time.Hour)

	eventBus := bus.New()

	// Build secret store: keychain with file fallback, then migrate legacy YAML files.
	secretStore := secretstore.NewFallbackStore(
		secretstore.NewKeychainStore(),
		secretstore.NewFileStore(ws.Root),
	)
	secretstore.MigrateFiles(secretStore, ws.Root)

	// Load secrets from the store and merge into the config.
	sec, err := config.LoadSecretsFromStore(secretStore)
	if err != nil {
		return nil, fmt.Errorf("loading secrets: %w", err)
	}
	config.ApplySecrets(cfg, sec)

	configStore := config.NewStore(cfg, cfgPath, secretStore)

	buildProvider := func(name, apiKey string) (provider.Provider, error) {
		if name == "" {
			return nil, fmt.Errorf("provider name is required")
		}
		return provider.NewOpenAI(name, apiKey), nil
	}

	sessionStore := session.NewStore(db)
	contextBuilder := agent.NewContextBuilder(ws.ProfilePath())
	taskStore := task.NewStore(db)
	if n, err := taskStore.CleanupStaleRuns(); err != nil {
		log.Printf("warning: failed to clean stale runs: %v", err)
	} else if n > 0 {
		log.Printf("cleaned up %d stale task run(s) from previous session", n)
	}
	// Backfill tasks with no user_id when there is exactly one user.
	if count, err := userStore.Count(); err == nil && count == 1 {
		if users, err := userStore.List(); err == nil && len(users) == 1 {
			if n, err := taskStore.BackfillUserID(users[0].ID); err != nil {
				log.Printf("warning: failed to backfill task user_id: %v", err)
			} else if n > 0 {
				slog.Info("backfilled task user_id", "count", n, "user", users[0].Email)
			}
		}
	}
	memoryStore := memory.NewStore(db)
	contentManager := memory.NewContentManager(ws.MemoriesDir())

	// Create embedding provider using the standard provider's connection.
	var nodeEmbedder *memory.NodeEmbedder
	if stdProv := cfg.Models.Standard.Provider; stdProv != "" && cfg.Memory.EmbeddingModel != "" {
		stdKey := cfg.ProviderAPIKey(stdProv)
		if stdKey != "" || provider.IsKeyless(stdProv) {
			embP := provider.NewOpenAI(stdProv, stdKey)
			nodeEmbedder = memory.NewNodeEmbedder(memoryStore, embP, cfg.Memory.EmbeddingModel, slog.Default())
		}
	}

	// Skills infrastructure.
	clawHub := skills.NewClawHub("", nil)
	skillsMgr := skills.NewManager(skills.ManagerConfig{
		ClawHub:     clawHub,
		Memory:      memoryStore,
		Content:     contentManager,
		EventBus:    eventBus,
		ConfigStore: configStore,
		SkillsDir:   ws.SkillsInstalledDir(),
		LearnedDir:  ws.SkillsLearnedDir(),
		Logger:      slog.Default(),
	})

	// Install bundled skills (idempotent).
	for _, sk := range skills.BundledSkills {
		if _, err := skillsMgr.EnsureBundled(sk); err != nil {
			slog.Warn("failed to install bundled skill", "slug", sk.Slug, "err", err)
		}
	}

	// Enricher: processes pending memory nodes by calling the LLM.
	enricher := worker.NewEnricher(memoryStore, contentManager, nil, eventBus, "", slog.Default(), nodeEmbedder, nil)
	enricher.Start(context.Background())

	// Profiler: revises the behavioral profile on ProfileRevisionDue events and
	// after every ProfileRegenThresh new memories.
	profiler := worker.NewProfiler(worker.ProfilerConfig{
		Memory:         memoryStore,
		EventBus:       eventBus,
		ProfilePath:    ws.ProfilePath(),
		Logger:         slog.Default(),
		RegenThreshold: cfg.Memory.ProfileRegenThresh,
	})
	profiler.Start(context.Background())

	// Consolidator: clusters enriched nodes into pattern nodes using adaptive thresholds.
	consolidator := worker.NewConsolidator(worker.ConsolidatorConfig{
		Store:        memoryStore,
		EventBus:     eventBus,
		Logger:       slog.Default(),
		MinThreshold: cfg.Memory.ConsolidationMin,
		MaxThreshold: cfg.Memory.ConsolidationMax,
		Scale:        cfg.Memory.ConsolidationScale,
	})
	consolidator.Start(context.Background())

	// ReflectionTrigger: monitors sessions and emits reflection events after
	// a message count threshold or idle timeout.
	reflectionTrigger := worker.NewReflectionTrigger(worker.TriggerConfig{
		EventBus: eventBus,
		Logger:   slog.Default(),
	})
	reflectionTrigger.Start(context.Background())

	// Reflector: classifies behavioral signals from conversations and creates
	// episode nodes that feed the enricher -> consolidator -> profiler pipeline.
	reflector := worker.NewReflector(sessionStore, memoryStore, contentManager, nil, eventBus, "", 0, slog.Default())
	reflector.Start(context.Background())

	toolsRegistry := tools.NewRegistry(ws.CustomToolsDir(), slog.Default())
	toolsRegistry.LoadCustomTools()

	// TaskStoreAdapter is created first; its Executor field is set after the
	// agent is ready, because the task.Executor needs agent.ProviderChat.
	taskAdapter := &tools.TaskStoreAdapter{Store: taskStore, EventBus: eventBus}

	memoryWriter := &tools.MemoryWriterAdapter{
		Store:    memoryStore,
		Content:  contentManager,
		Embedder: nodeEmbedder,
		EventBus: eventBus,
		// Retriever is set after retriever is created below.
	}

	sensitivePaths := cfg.Security.SensitivePaths
	if len(sensitivePaths) == 0 {
		sensitivePaths = security.DefaultSensitivePaths
	}

	dangerousCommands := cfg.Security.DangerousCommands
	if len(dangerousCommands) == 0 {
		dangerousCommands = security.DefaultDangerousCommands
	}

	runner, err := sandbox.NewRunner(cfg.Security.Sandbox, cfg.Security.MaxOutputBytes, slog.Default())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("creating sandbox runner: %w", err)
	}

	mcpManager := mcp.NewManager(ws.MCPConfigPath(), secretStore, eventBus, slog.Default())
	if err := mcpManager.LoadConfig(); err != nil {
		slog.Warn("MCP config load failed", "error", err)
	}

	toolExecutor := tools.NewExecutor(
		toolsRegistry,
		ws.Root,
		runner,
		taskAdapter,
		&tools.SkillManagerAdapter{Manager: skillsMgr},
		memoryWriter,
		slog.Default(),
		db,
		sensitivePaths,
		dangerousCommands,
		cfg.Security.AllowedDomains,
	)

	skillsMgr.DomainSetter = toolExecutor
	toolExecutor.SetDomainAllowlister(configStore)
	toolExecutor.SetShellDir(ws.SandboxDir())
	toolExecutor.SetMCPManager(mcpManager)

	// Connector runtime.
	connectorSettingsPath := filepath.Join(ws.Root, "connector_settings.yaml")
	connectorMgr := connector.NewManager(ws.ConnectorsDir(), secretStore, connectorSettingsPath, cfg.Server.Port)

	// Load embedded default connectors.
	defaultFS := connector.EmbeddedDefaults()
	if entries, err := defaultFS.ReadDir("defaults"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			data, err := defaultFS.ReadFile(filepath.Join("defaults", entry.Name(), "connector.yaml"))
			if err != nil {
				continue
			}
			connectorMgr.LoadEmbedded(entry.Name(), data)
		}
	}

	// Load workspace connectors (override embedded).
	connectorMgr.LoadAll()

	// Register connector tools.
	for _, def := range connectorMgr.ToolDefs() {
		toolsRegistry.Register(def)
	}

	toolExecutor.SetConnectorCaller(connectorMgr)

	// Browser connector (CDP-based Chrome control).
	browserConn := browser.NewConnector(ws.Root, slog.Default())
	toolExecutor.SetBrowserConnector(browserConn)

	if userStore != nil {
		toolExecutor.SetUserLister(&userListerAdapter{store: userStore})
		toolExecutor.SetMemoryToggler(memoryWriter)
		taskAdapter.UserLister = &userListerAdapter{store: userStore}
	}

	retriever := memory.NewRetriever(memory.RetrieverConfig{
		Store:         memoryStore,
		Content:       contentManager,
		Logger:        slog.Default(),
		RecencyAlpha:  cfg.Memory.RecencyAlpha,
		RecencyLambda: cfg.Memory.RecencyLambda,
		ContextWindow: cfg.Memory.ContextWindow,
		TopK:          cfg.Memory.RetrievalTopK,
	})
	if nodeEmbedder != nil {
		stdProv := cfg.Models.Standard.Provider
		embP := provider.NewOpenAI(stdProv, cfg.ProviderAPIKey(stdProv))
		retriever.SetEmbedder(embP, cfg.Memory.EmbeddingModel)
	}

	// Wire retriever back into enricher and memoryWriter now that it exists.
	enricher.SetRetriever(retriever)
	memoryWriter.Retriever = retriever

	// Only create a budget guard when at least one limit is configured.
	var budgetGuard *budget.Guard
	if cfg.Resources.DailyBudgetStandard > 0 || cfg.Resources.DailyBudgetCheap > 0 ||
		cfg.Resources.StandardModelRPM > 0 || cfg.Resources.CheapModelRPM > 0 {
		budgetGuard = budget.NewGuard(db, budget.Limits{
			DailyBudgetStandard: cfg.Resources.DailyBudgetStandard,
			DailyBudgetCheap:    cfg.Resources.DailyBudgetCheap,
			StandardModelRPM:    cfg.Resources.StandardModelRPM,
			CheapModelRPM:       cfg.Resources.CheapModelRPM,
		})
	}

	a := agent.New(agent.Config{
		Sessions:       sessionStore,
		ContextBuilder: contextBuilder,
		ToolExecutor:   toolExecutor,
		Retriever: &agent.RetrieverAdapter{
			Retriever:    retriever,
			NameResolver: buildNameResolver(userStore),
		},
		Tools:          func() []provider.Tool {
			t := toolsRegistry.ProviderTools()
			if browserConn.IsEnabled() {
				for _, td := range browser.ToolDefs() {
					t = append(t, td.ProviderTool())
				}
			}
			return t
		}(),
		EventBus:       eventBus,
		Model:          cfg.Models.Standard.Model,
		UsageRecorder:  db,
		BudgetGuard:    budgetGuard,
		MCPServers:     &mcpServerAdapter{mgr: mcpManager},
		Connectors:     &connectorStatusAdapter{mgr: connectorMgr},
		Skills:         &skillListerAdapter{mgr: skillsMgr},
	})

	mcpManager.SetToolRegistrationCallback(func() {
		// Build lookups of server metadata and tools partitioned by server.
		servers := mcpManager.Servers()
		serverInstructions := map[string]string{}
		for _, s := range servers {
			serverInstructions[s.Name] = s.Instructions
		}

		toolsByServer := map[string][]mcp.ToolInfo{}
		for _, tool := range mcpManager.AllTools() {
			toolsByServer[tool.ServerName] = append(toolsByServer[tool.ServerName], tool)
			toolsRegistry.Register(tools.ToolDef{
				Name:        tool.QualifiedName,
				Description: mcp.EnrichToolDescription(tool.ServerName, serverInstructions[tool.ServerName], tool.Description),
				Parameters:  tool.InputSchema,
				MCPServer:   tool.ServerName,
				MCPToolName: tool.Name,
			})
		}
		a.SetTools(toolsRegistry.ProviderTools())

		// Generate or update a skill for each running server.
		for _, srv := range servers {
			serverTools := toolsByServer[srv.Name]
			if srv.Status != mcp.StatusRunning || len(serverTools) == 0 {
				continue
			}
			slug := mcp.MCPSkillSlug(srv.Name)
			content := mcp.GenerateSkillContent(srv.Name, srv.Instructions, serverTools)
			summary := fmt.Sprintf("MCP server %q tool reference", srv.Name)
			if srv.Instructions != "" {
				summary = srv.Instructions
			}
			nodeID, err := skillsMgr.CreateSkill(slug, srv.Name+" (MCP)", summary, content)
			if err != nil {
				slog.Warn("mcp: skill gen: failed to create skill", "server", srv.Name, "err", err)
				continue
			}
			// Update content if skill already existed (instructions or tools may have changed).
			if _, err := skillsMgr.UpdateSkill(nodeID, "", "", content); err != nil {
				slog.Warn("mcp: skill gen: failed to update skill", "server", srv.Name, "err", err)
			}
		}
	})

	// When the browser connector enables/disables, refresh the agent's tool list.
	browserConn.OnToolsChanged(func() {
		combined := toolsRegistry.ProviderTools()
		if browserConn.IsEnabled() {
			for _, td := range browser.ToolDefs() {
				combined = append(combined, td.ProviderTool())
			}
		}
		a.SetTools(combined)
	})

	// If the browser connector was previously enabled, reconnect on startup.
	// This runs after the callback is registered so the agent gets the tools.
	if browserConn.IsEnabled() {
		go func() {
			if err := browserConn.Enable(); err != nil {
				slog.Warn("browser connector: auto-enable failed", "error", err)
			}
		}()
	}

	// Wire the task executor now that the agent exists.
	modelResolver := func(tier string) string {
		current := configStore.Get()
		switch tier {
		case "standard":
			return current.Models.Standard.Model
		case "cheap":
			if current.Models.Cheap.Model != "" {
				return current.Models.Cheap.Model
			}
			return current.Models.Standard.Model
		default:
			return tier
		}
	}
	taskExecutor := task.NewExecutor(taskStore, a.RunTask, modelResolver, eventBus, slog.Default())
	taskAdapter.Executor = taskExecutor

	// Cron scheduler.
	taskScheduler := task.NewScheduler(taskStore, func(t task.Task) {
		if _, err := taskExecutor.Execute(context.Background(), t, task.TriggerCron); err != nil {
			slog.Error("scheduled task execution failed", "task", t.Name, "error", err)
		}
	}, eventBus, slog.Default())
	if err := taskScheduler.Start(); err != nil {
		log.Printf("warning: task scheduler start failed: %v", err)
	}
	taskAdapter.Scheduler = taskScheduler

	// If a provider is already configured, activate it immediately.
	stdProvider := cfg.Models.Standard.Provider
	stdKey := cfg.ProviderAPIKey(stdProvider)
	if stdProvider != "" && (stdKey != "" || provider.IsKeyless(stdProvider)) {
		if stdP, err := buildProvider(stdProvider, stdKey); err == nil {
			a.SetProvider(stdP, cfg.Models.Standard.Model)

			cheapProvider := cfg.Models.Cheap.Provider
			cheapModel := cfg.Models.Cheap.Model
			cheapP := stdP
			if cheapProvider != "" && cheapProvider != stdProvider {
				cheapKey := cfg.ProviderAPIKey(cheapProvider)
				if cheapKey != "" || provider.IsKeyless(cheapProvider) {
					if cp, err := buildProvider(cheapProvider, cheapKey); err == nil {
						cheapP = cp
					}
				}
			}
			if cheapModel == "" {
				cheapModel = cfg.Models.Standard.Model
			}
			if cheapP != stdP {
				a.SetModelProvider(cheapModel, cheapP)
			}
			retriever.SetProvider(cheapP, cheapModel)
			retriever.SetStandardProvider(stdP, cfg.Models.Standard.Model)
			enricher.SetProvider(cheapP, cheapModel)
			profiler.SetProvider(stdP, cfg.Models.Standard.Model)
			consolidator.SetProvider(cheapP, cheapModel)
			reflector.SetProvider(cheapP, cheapModel)
		}
	}

	// Backfill embeddings for any existing nodes that lack them.
	go func() {
		if nodeEmbedder != nil {
			worker.RunBackfill(context.Background(), memoryStore, nodeEmbedder, 50)
			retriever.InvalidateCache()
		}
	}()

	notificationStore := notification.NewStore(db)
	toolExecutor.SetUserNotifier(&tools.NotifierAdapter{
		Notifications: notificationStore,
		EventBus:      eventBus,
	})
	pushStore := push.NewStore(db)

	// Web chat channel.
	webChannel := channel.NewWebChannel(
		func(ctx context.Context, msg channel.IncomingMessage) (channel.HandlerResponse, error) {
			var profileOverrides string
			var userName string
			if userStore != nil && msg.UserID != "" {
				if u, err := userStore.Get(msg.UserID); err == nil {
					profileOverrides = u.ProfileOverrides
					userName = u.Name
				}
			}
			resp, err := a.Chat(ctx, agent.ChatRequest{
				SessionKey:       msg.SessionKey,
				Channel:          msg.Channel,
				ChatID:           msg.ChatID,
				UserID:           msg.UserID,
				UserName:         userName,
				Private:          msg.Private,
				Message:          msg.Text,
				ProfileOverrides: profileOverrides,
			})
			if err != nil {
				return channel.HandlerResponse{}, err
			}
			hr := channel.HandlerResponse{Content: resp.Content}
			if resp.ToolsUsed != nil {
				if data, err := json.Marshal(resp.ToolsUsed); err == nil {
					hr.ToolsUsed = string(data)
				}
			}
			return hr, nil
		},
		eventBus,
		sessionStore,
		notificationStore,
		func(id int64) string {
			if t, err := taskStore.GetTask(id); err == nil {
				return t.Name
			}
			return ""
		},
		slog.Default(),
	)
	if userStore != nil {
		webChannel.SetUserIDsFunc(func() []string {
			users, err := userStore.List()
			if err != nil {
				return nil
			}
			ids := make([]string, len(users))
			for i, u := range users {
				ids[i] = u.ID
			}
			return ids
		})
	}
	if err := webChannel.Start(context.Background()); err != nil {
		slog.Error("web channel start failed", "error", err)
	}

	pushSender := push.NewSender(pushStore, slog.Default())
	pushDispatcher := push.NewDispatcher(pushSender, eventBus, notificationStore, sessionStore, slog.Default())
	pushDispatcher.Start()

	// Telegram chat channel.
	telegramChannel := channel.NewTelegramChannel(
		func(ctx context.Context, msg channel.IncomingMessage) (channel.HandlerResponse, error) {
			var userName string
			if userStore != nil && msg.UserID != "" {
				if u, err := userStore.Get(msg.UserID); err == nil {
					userName = u.Name
				}
			}
			resp, err := a.Chat(ctx, agent.ChatRequest{
				SessionKey: msg.SessionKey,
				Channel:    msg.Channel,
				ChatID:     msg.ChatID,
				UserID:     msg.UserID,
				UserName:   userName,
				Message:    msg.Text,
			})
			if err != nil {
				return channel.HandlerResponse{}, err
			}
			return channel.HandlerResponse{Content: resp.Content}, nil
		},
		eventBus,
		sessionStore,
		configStore,
		slog.Default(),
	)
	if err := telegramChannel.Start(context.Background()); err != nil {
		log.Printf("warning: telegram channel start failed: %v", err)
	}

	ollamaClient := ollama.New("")

	appUpdater := updater.New(updater.Config{
		Owner:          "cogitatorai",
		Repo:           "cogitator",
		Current:        version.Version,
		CachePath:      filepath.Join(ws.Root, "update_cache.json"),
		SkippedVersion: cfg.Update.SkippedVersion,
	})

	// Determine the dashboard serving source.
	dashboardDir := opts.DashboardDir
	if dashboardDir == "" {
		if v := os.Getenv("COGITATOR_DASHBOARD_DIR"); v != "" {
			dashboardDir = v
		} else if isSaaS {
			dashboardDir = defaultSaaSDashboardDir
		}
	}
	// Auto-detect: try ../cogitator/dashboard/dist relative to the executable.
	if dashboardDir == "" && opts.DashboardFS == nil {
		if exe, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(exe), "..", "cogitator", "dashboard", "dist")
			if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
				dashboardDir = candidate
			}
		}
	}

	// SaaS-specific subsystems: metrics collection, drain manager, internal auth.
	var metricsRing *metrics.Ring
	var drainMgr *drain.Manager
	var internalSecret string
	if isSaaS {
		internalSecret = os.Getenv("COGITATOR_INTERNAL_SECRET")
		if internalSecret == "" {
			db.Close()
			return nil, fmt.Errorf("COGITATOR_INTERNAL_SECRET is required in SaaS mode (empty value would bypass internal auth)")
		}
		metricsRing = metrics.NewRing(1000)
		drainMgr = drain.New()
	}

	// Social sign-in credentials: prefer build-time ldflags, fall back to env vars.
	googleClientID := connector.GoogleClientID
	googleClientSecret := connector.GoogleClientSecret
	if googleClientID == "" {
		googleClientID = os.Getenv("GOOGLE_CLIENT_ID")
	}
	if googleClientSecret == "" {
		googleClientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
	}

	appleAudiences := []string{social.AppleServicesID, social.AppleMobileBundleID}
	appleAudiences = append(appleAudiences, debugAppleAudiences...)
	socialVerifier := social.NewVerifier(googleClientID, appleAudiences...)

	routerCfg := api.RouterConfig{
		Agent:           a,
		Sessions:        sessionStore,
		Memory:          memoryStore,
		Tasks:           taskStore,
		TaskExecutor:    taskExecutor,
		Skills:          skillsMgr,
		Tools:           toolsRegistry,
		Web:             webChannel,
		Telegram:        telegramChannel,
		EventBus:        eventBus,
		ConfigStore:     configStore,
		ProviderFactory: buildProvider,
		Retriever:       retriever,
		NodeEmbedder:    nodeEmbedder,
		Enricher:        enricher,
		EnrichStatus:    enricher,
		Scheduler:       taskScheduler,
		DB:              db,
		DashboardDir:    dashboardDir,
		DashboardFS:     opts.DashboardFS,
		Ollama:          ollamaClient,
		DomainSetter:    toolExecutor,
		Updater:         appUpdater,
		JWTService:      jwtSvc,
		Users:           userStore,
		Notifications:   notificationStore,
		PushTokens:      pushStore,
		ServerPort:      cfg.Server.Port,
		MCP:              mcpManager,
		Connectors:       connectorMgr,
		BrowserConnector: browserConn,
		Store:           secretStore,
		SocialVerifier:  socialVerifier,
		GoogleClientID:     googleClientID,
		GoogleClientSecret: googleClientSecret,
		AppleServicesID:    social.AppleServicesID,
		MetricsRing:        metricsRing,
		InternalSecret:     internalSecret,
		DrainManager:       drainMgr,
	}

	// Use an indirect shutdown function so the router can trigger server shutdown
	// for the auto-updater (the server doesn't exist yet at router creation time).
	var srv *Server
	routerCfg.ShutdownFn = func() {
		if srv != nil {
			srv.ShutdownWithTimeout(5 * time.Second)
		}
	}
	router := api.NewRouter(routerCfg)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	srv = &Server{
		httpServer:    &http.Server{Addr: addr, Handler: router},
		addr:          addr,
		wsRoot:        ws.Root,
		configStore:   configStore,
		db:            db,
		eventBus:      eventBus,
		enricher:          enricher,
		profiler:          profiler,
		consolidator:      consolidator,
		reflector:         reflector,
		reflectionTrigger: reflectionTrigger,
		webChannel:    webChannel,
		teleChannel:   telegramChannel,
		taskScheduler: taskScheduler,
		updater:       appUpdater,
		mcpManager:       mcpManager,
		browserConnector: browserConn,
		pushDispatcher:   pushDispatcher,
	}

	// Start the heartbeat goroutine in SaaS mode.
	if isSaaS {
		orchestratorURL := os.Getenv("COGITATOR_ORCHESTRATOR_URL")
		tenantID := os.Getenv("COGITATOR_TENANT_ID")
		hb := heartbeat.New(heartbeat.Config{
			OrchestratorURL: orchestratorURL,
			TenantID:        tenantID,
			InternalSecret:  internalSecret,
			Ring:            metricsRing,
		})
		hb.Start()
		srv.heartbeat = hb
		srv.orchestratorURL = orchestratorURL
		srv.tenantID = tenantID
		srv.saasInternalSecret = internalSecret
	}

	return srv, nil
}

// Addr returns the address the server will listen on (e.g. "127.0.0.1:8484").
func (s *Server) Addr() string { return s.addr }

// Start begins serving HTTP in a background goroutine. It returns immediately.
// If the configured port is unavailable, the server falls back to an
// OS-assigned port and persists the actual port to the config file so
// subsequent launches (and desktop clients reading the config) reuse it.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		// Port busy: fall back to an OS-assigned port on the same host.
		host, _, _ := net.SplitHostPort(s.addr)
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return fmt.Errorf("listen %s (and fallback :0): %w", s.addr, err)
		}
		// Persist the actual port so the wrapper and future restarts find it.
		actual := ln.Addr().(*net.TCPAddr).Port
		s.addr = net.JoinHostPort(host, fmt.Sprint(actual))
		s.httpServer.Addr = s.addr
		if s.configStore != nil {
			cfg := s.configStore.Get()
			cfg.Server.Port = actual
			if saveErr := s.configStore.Save(cfg); saveErr != nil {
				log.Printf("warning: could not persist fallback port %d: %v", actual, saveErr)
			}
		}
		log.Printf("configured port in use; fell back to %s", s.addr)
	}
	log.Printf("cogitator %s listening on %s (workspace: %s)", version.Version, s.addr, s.wsRoot)
	s.updater.Start(context.Background())
	go func() {
		if err := s.httpServer.Serve(ln); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the HTTP server and all subsystems.
func (s *Server) Shutdown(ctx context.Context) {
	// In SaaS mode, notify the orchestrator of the next scheduled wake time
	// before stopping, so scale-to-zero does not miss cron tasks.
	if s.heartbeat != nil {
		if wakeAt, ok := s.taskScheduler.NextScheduledTime(); ok {
			if err := heartbeat.NotifyWakeTime(s.orchestratorURL, s.tenantID, s.saasInternalSecret, wakeAt); err != nil {
				slog.Warn("failed to notify orchestrator of next wake time", "error", err)
			}
		}
		s.heartbeat.Stop()
	}
	if s.pushDispatcher != nil {
		s.pushDispatcher.Stop()
	}
	s.updater.Stop()
	s.httpServer.Shutdown(ctx)
	s.taskScheduler.Stop()
	s.teleChannel.Stop()
	s.webChannel.Stop()
	s.enricher.Stop()
	s.profiler.Stop()
	s.consolidator.Stop()
	s.reflector.Stop()
	s.reflectionTrigger.Stop()
	if s.mcpManager != nil {
		s.mcpManager.StopAll()
	}
	if s.browserConnector != nil {
		s.browserConnector.Disable()
	}
	s.eventBus.Close()
	s.db.Close()
}

// ShutdownWithTimeout is a convenience that creates a context with the given
// timeout and calls Shutdown.
func (s *Server) ShutdownWithTimeout(d time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	s.Shutdown(ctx)
}

// connectorStatusAdapter adapts the connector Manager to the agent's ConnectorStatusProvider interface.
type connectorStatusAdapter struct {
	mgr *connector.Manager
}

func (a *connectorStatusAdapter) ConnectorStatuses(userID string) []agent.ConnectorStatus {
	infos := a.mgr.List()
	statuses := a.mgr.ConnectorStatuses(userID)
	out := make([]agent.ConnectorStatus, len(infos))
	for i, info := range infos {
		out[i] = agent.ConnectorStatus{
			Name:        info.Name,
			DisplayName: info.DisplayName,
			Connected:   statuses[info.Name],
		}
	}
	return out
}

// mcpServerAdapter adapts the MCP Manager to the agent's MCPServerLister interface.
type mcpServerAdapter struct {
	mgr interface{ Servers() []mcp.ServerStatus }
}

func (a *mcpServerAdapter) Servers() []agent.MCPServerInfo {
	statuses := a.mgr.Servers()
	out := make([]agent.MCPServerInfo, len(statuses))
	for i, s := range statuses {
		out[i] = agent.MCPServerInfo{
			Name:         s.Name,
			Status:       s.Status,
			ToolCount:    s.ToolCount,
			Instructions: s.Instructions,
		}
	}
	return out
}

// skillListerAdapter adapts the skills.Manager to the agent's SkillLister interface.
type skillListerAdapter struct {
	mgr *skills.Manager
}

func (a *skillListerAdapter) SkillSummaries() []agent.SkillSummary {
	nodes, err := a.mgr.List()
	if err != nil {
		return nil
	}
	out := make([]agent.SkillSummary, len(nodes))
	for i, n := range nodes {
		out[i] = agent.SkillSummary{NodeID: n.ID, Name: n.Title, Summary: n.Summary}
	}
	return out
}

// userListerAdapter adapts the user.Store to the tools.UserLister interface.
type userListerAdapter struct {
	store *user.Store
}

func (a *userListerAdapter) ListOtherUsers(callerID string) ([]tools.UserInfo, error) {
	users, err := a.store.List()
	if err != nil {
		return nil, err
	}
	var result []tools.UserInfo
	for _, u := range users {
		if u.ID != callerID {
			result = append(result, tools.UserInfo{ID: u.ID, Name: u.Name})
		}
	}
	return result, nil
}

func (a *userListerAdapter) ListAllUsers() ([]tools.UserInfo, error) {
	users, err := a.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]tools.UserInfo, len(users))
	for i, u := range users {
		out[i] = tools.UserInfo{ID: u.ID, Name: u.Name}
	}
	return out, nil
}

// buildNameResolver returns a NameResolver that looks up user display names
// from the user store. Returns nil when no store is available.
func buildNameResolver(users *user.Store) memory.NameResolver {
	if users == nil {
		return nil
	}
	return func(userID string) string {
		u, err := users.Get(userID)
		if err != nil {
			return ""
		}
		return u.Name
	}
}

