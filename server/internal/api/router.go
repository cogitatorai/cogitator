package api

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/connector/browser"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/channel"
	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/drain"
	"github.com/cogitatorai/cogitator/server/internal/mcp"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/metrics"
	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/push"
	"github.com/cogitatorai/cogitator/server/internal/ollama"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/secretstore"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/skills"
	"github.com/cogitatorai/cogitator/server/internal/social"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"github.com/cogitatorai/cogitator/server/internal/tools"
	"github.com/cogitatorai/cogitator/server/internal/updater"
	"github.com/cogitatorai/cogitator/server/internal/user"
	"github.com/cogitatorai/cogitator/server/internal/voice"
)

// ProviderSetter is implemented by components that accept a hot-swapped LLM provider.
type ProviderSetter interface {
	SetProvider(p provider.Provider, model string)
}

// EnrichmentStatus is implemented by the enricher to report whether it is
// actively processing nodes.
type EnrichmentStatus interface {
	IsActive() bool
}

// SocialVerifier verifies a social ID token for a given provider and returns
// the verified identity. It is satisfied by *social.Verifier and by test mocks.
type SocialVerifier interface {
	Verify(ctx context.Context, provider, idToken string) (*social.VerifiedIdentity, error)
}

// TaskScheduler exposes next-run information from the cron scheduler.
type TaskScheduler interface {
	NextRunTimes() map[int64]time.Time
}

// DomainAllowlistSetter is implemented by the tool executor to hot-swap
// the list of domains permitted for network commands.
type DomainAllowlistSetter interface {
	SetAllowedDomains(domains []string)
}

type Router struct {
	mux             *http.ServeMux
	handler         http.Handler
	agent           *agent.Agent
	sessions        *session.Store
	memory          *memory.Store
	tasks           *task.Store
	taskExecutor    *task.Executor
	skills          *skills.Manager
	tools           *tools.Registry
	web             *channel.WebChannel
	telegram        *channel.TelegramChannel
	configStore     *config.Store
	providerFactory ProviderFactory
	eventBus        *bus.Bus
	retriever       *memory.Retriever
	nodeEmbedder    *memory.NodeEmbedder
	enricher        ProviderSetter
	enrichStatus    EnrichmentStatus
	scheduler       TaskScheduler
	db              *database.DB
	dashboardDir    string
	dashboardFS     fs.FS
	ollama          *ollama.Client
	domainSetter    DomainAllowlistSetter
	updater         *updater.Updater
	shutdownFn      func()
	mcp             *mcp.Manager
	connectors        ConnectorManager
	browserConnector  *browser.Connector
	jwtSvc            *auth.JWTService
	users           *user.Store
	socialVerifier  SocialVerifier
	googleClientID     string
	googleClientSecret string
	appleServicesID    string
	serverPort         int
	publicURL          string
	notifications   *notification.Store
	pushTokens      *push.Store
	store           secretstore.SecretStore
	metricsRing     *metrics.Ring
	internalSecret  string
	drainManager    *drain.Manager
	reembedCancel        context.CancelFunc // cancels any in-flight re-embed goroutine
	voiceRegistry        *voice.Registry
	voiceRegistryBuilder func(*config.Config) *voice.Registry
	isSaaS               bool
}

type RouterConfig struct {
	Agent           *agent.Agent
	Sessions        *session.Store
	Memory          *memory.Store
	Tasks           *task.Store
	TaskExecutor    *task.Executor
	Skills          *skills.Manager
	Tools           *tools.Registry
	Web             *channel.WebChannel
	Telegram        *channel.TelegramChannel
	ConfigStore     *config.Store
	ProviderFactory ProviderFactory
	EventBus        *bus.Bus
	Retriever       *memory.Retriever
	NodeEmbedder    *memory.NodeEmbedder
	Enricher        ProviderSetter
	EnrichStatus    EnrichmentStatus
	Scheduler       TaskScheduler
	DB              *database.DB
	DashboardDir    string
	DashboardFS     fs.FS
	Ollama          *ollama.Client
	DomainSetter    DomainAllowlistSetter
	Updater         *updater.Updater
	ShutdownFn      func()
	MCP             *mcp.Manager
	Connectors       ConnectorManager
	BrowserConnector *browser.Connector
	JWTService       *auth.JWTService
	ServerPort      int
	PublicURL       string
	Users           *user.Store
	SocialVerifier  SocialVerifier
	GoogleClientID     string
	GoogleClientSecret string
	AppleServicesID    string
	Notifications   *notification.Store
	PushTokens      *push.Store
	Store           secretstore.SecretStore
	MetricsRing     *metrics.Ring
	InternalSecret  string
	DrainManager    *drain.Manager
	VoiceRegistry        *voice.Registry
	VoiceRegistryBuilder func(*config.Config) *voice.Registry
	IsSaaS               bool
}

func NewRouter(cfg RouterConfig) *Router {
	r := &Router{
		mux:             http.NewServeMux(),
		agent:           cfg.Agent,
		sessions:        cfg.Sessions,
		memory:          cfg.Memory,
		tasks:           cfg.Tasks,
		taskExecutor:    cfg.TaskExecutor,
		skills:          cfg.Skills,
		tools:           cfg.Tools,
		web:             cfg.Web,
		telegram:        cfg.Telegram,
		configStore:     cfg.ConfigStore,
		providerFactory: cfg.ProviderFactory,
		eventBus:        cfg.EventBus,
		retriever:       cfg.Retriever,
		nodeEmbedder:    cfg.NodeEmbedder,
		enricher:        cfg.Enricher,
		enrichStatus:    cfg.EnrichStatus,
		scheduler:       cfg.Scheduler,
		db:              cfg.DB,
		dashboardDir:    cfg.DashboardDir,
		dashboardFS:     cfg.DashboardFS,
		ollama:          cfg.Ollama,
		domainSetter:    cfg.DomainSetter,
		updater:         cfg.Updater,
		shutdownFn:      cfg.ShutdownFn,
		mcp:             cfg.MCP,
		connectors:       cfg.Connectors,
		browserConnector: cfg.BrowserConnector,
		jwtSvc:           cfg.JWTService,
		users:           cfg.Users,
		socialVerifier:  cfg.SocialVerifier,
		googleClientID:     cfg.GoogleClientID,
		googleClientSecret: cfg.GoogleClientSecret,
		appleServicesID:    cfg.AppleServicesID,
		serverPort:         cfg.ServerPort,
		publicURL:          cfg.PublicURL,
		notifications:   cfg.Notifications,
		pushTokens:      cfg.PushTokens,
		store:           cfg.Store,
		metricsRing:     cfg.MetricsRing,
		internalSecret:  cfg.InternalSecret,
		drainManager:    cfg.DrainManager,
		voiceRegistry:        cfg.VoiceRegistry,
		voiceRegistryBuilder: cfg.VoiceRegistryBuilder,
		isSaaS:               cfg.IsSaaS,
	}
	r.registerRoutes()

	// Compose middleware chain: CORS (outermost) -> auth -> mux (innermost).
	var handler http.Handler = r.mux
	if cfg.JWTService != nil {
		handler = jwtAuthMiddleware(cfg.JWTService, r.internalSecret != "", handler)
	}
	handler = corsMiddleware(cfg.ServerPort, handler)
	if cfg.MetricsRing != nil {
		handler = metrics.Middleware(cfg.MetricsRing)(handler)
	}
	if cfg.DrainManager != nil {
		handler = cfg.DrainManager.Middleware()(handler)
	}
	// In SaaS mode, ensure requests reach the correct tenant machine.
	// Fly's proxy may route a request to any machine in the app; if the
	// Host header doesn't match this tenant's hostname, reply with
	// fly-replay so the proxy retries on another instance.
	if r.isSaaS && r.publicURL != "" {
		if u, err := url.Parse(r.publicURL); err == nil && u.Host != "" {
			expectedHost := u.Host
			handler = flyReplayMiddleware(expectedHost, handler)
		}
	}
	r.handler = handler

	return r
}

func (r *Router) registerRoutes() {
	r.mux.HandleFunc("GET /api/health", r.handleHealth)
	r.mux.HandleFunc("GET /api/status", r.handleSystemStatus)
	r.mux.HandleFunc("GET /api/auth/providers", r.handleAuthProviders)
	r.mux.HandleFunc("GET /api/connectors/callback", r.handleConnectorCallback)

	if r.users != nil && r.jwtSvc != nil {
		r.mux.HandleFunc("POST /api/auth/register", r.handleRegister)
		r.mux.HandleFunc("POST /api/auth/login", r.handleLogin)
		r.mux.HandleFunc("POST /api/auth/refresh", r.handleRefresh)
		r.mux.HandleFunc("POST /api/auth/logout", r.handleLogout)
		r.mux.HandleFunc("GET /api/auth/needs-setup", r.handleNeedsSetup)
		r.mux.HandleFunc("POST /api/auth/setup", r.handleSetup)

		if r.socialVerifier != nil {
			r.mux.HandleFunc("POST /api/auth/social", r.handleSocialAuth)
			r.mux.HandleFunc("GET /api/auth/google/start", r.handleGoogleAuthStart)
			r.mux.HandleFunc("GET /api/auth/google/callback", r.handleGoogleCallback)
			r.mux.HandleFunc("GET /api/auth/claim/{id}", r.handleAuthClaim)
		}

		// User management (admin only).
		adminOnly := requireRole("admin")
		r.mux.Handle("GET /api/users", adminOnly(http.HandlerFunc(r.handleListUsers)))
		r.mux.Handle("GET /api/users/{id}", adminOnly(http.HandlerFunc(r.handleGetUser)))
		r.mux.Handle("PUT /api/users/{id}/role", adminOnly(http.HandlerFunc(r.handleUpdateUserRole)))
		r.mux.Handle("PUT /api/users/{id}/password", adminOnly(http.HandlerFunc(r.handleResetPassword)))
		r.mux.Handle("DELETE /api/users/{id}", adminOnly(http.HandlerFunc(r.handleDeleteUser)))

		// Invite codes (admin + moderator).
		adminOrMod := requireRole("admin", "moderator")
		r.mux.Handle("POST /api/invite-codes", adminOrMod(http.HandlerFunc(r.handleCreateInviteCode)))
		r.mux.Handle("GET /api/invite-codes", adminOrMod(http.HandlerFunc(r.handleListInviteCodes)))
		r.mux.Handle("DELETE /api/invite-codes/{code}", adminOrMod(http.HandlerFunc(r.handleDeleteInviteCode)))

		// Profile (any authenticated user).
		r.mux.HandleFunc("GET /api/me", r.handleGetMe)
		r.mux.HandleFunc("PUT /api/me", r.handleUpdateMe)
		r.mux.HandleFunc("GET /api/me/profile", r.handleGetProfile)
		r.mux.HandleFunc("PUT /api/me/profile", r.handleUpdateProfile)

		if r.socialVerifier != nil {
			r.mux.HandleFunc("POST /api/account/link", r.handleLinkOAuth)
			r.mux.HandleFunc("DELETE /api/account/link/{provider}", r.handleUnlinkOAuth)
			r.mux.HandleFunc("GET /api/account/links", r.handleListOAuthLinks)
		}
	}

	if r.updater != nil {
		r.mux.HandleFunc("GET /api/version", r.handleGetVersion)
		r.mux.HandleFunc("POST /api/version/check", r.handleCheckVersion)
		r.mux.HandleFunc("POST /api/version/download", r.handleDownloadUpdate)
		r.mux.HandleFunc("POST /api/version/restart", r.handleRestartUpdate)
		r.mux.HandleFunc("POST /api/version/skip", r.handleSkipVersion)
	}

	if r.agent != nil {
		r.mux.HandleFunc("POST /api/chat", r.handleChat)
		r.mux.HandleFunc("POST /api/chat/message", r.handleChatWithFile)
		r.mux.HandleFunc("POST /api/chat/voice", r.handleVoice)
	}

	if r.sessions != nil {
		r.mux.HandleFunc("GET /api/sessions", r.handleListSessions)
		r.mux.HandleFunc("GET /api/sessions/{key}", r.handleGetSession)
		r.mux.HandleFunc("PUT /api/sessions/{key}/activate", r.handleActivateSession)
		r.mux.HandleFunc("DELETE /api/sessions/{key}", r.handleDeleteSession)
		r.mux.HandleFunc("DELETE /api/sessions/{key}/messages", r.handleClearMessages)
		r.mux.HandleFunc("DELETE /api/sessions/{key}/messages/{id}", r.handleDeleteMessage)
	}

	if r.memory != nil {
		r.mux.HandleFunc("GET /api/memory/stats", r.handleMemoryStats)
		r.mux.HandleFunc("GET /api/memory/nodes", r.handleListMemoryNodes)
		r.mux.HandleFunc("POST /api/memory/nodes", r.handleCreateMemoryNode)
		r.mux.HandleFunc("GET /api/memory/nodes/{id}", r.handleGetMemoryNode)
		r.mux.HandleFunc("DELETE /api/memory/nodes/{id}", r.handleDeleteMemoryNode)
		r.mux.HandleFunc("GET /api/memory/nodes/{id}/edges", r.handleGetMemoryEdges)
		r.mux.HandleFunc("GET /api/memory/nodes/{id}/connected", r.handleGetConnectedNodes)
		r.mux.HandleFunc("PATCH /api/memory/nodes/{id}/pin", r.handlePinNode)
		r.mux.HandleFunc("PATCH /api/memory/nodes/{id}/privacy", r.handleToggleNodePrivacy)
		r.mux.HandleFunc("GET /api/memory/graph", r.handleMemoryGraph)
		r.mux.HandleFunc("POST /api/memory/enrich", r.handleTriggerEnrichment)
	}

	if r.tasks != nil {
		r.mux.HandleFunc("GET /api/tasks", r.handleListTasks)
		r.mux.HandleFunc("POST /api/tasks", r.handleCreateTask)
		r.mux.HandleFunc("GET /api/tasks/{id}", r.handleGetTask)
		r.mux.HandleFunc("PUT /api/tasks/{id}", r.handleUpdateTask)
		r.mux.HandleFunc("DELETE /api/tasks/{id}", r.handleDeleteTask)
		r.mux.HandleFunc("POST /api/tasks/{id}/trigger", r.handleTriggerTask)
		r.mux.HandleFunc("GET /api/tasks/{id}/runs", r.handleListTaskRuns)
		r.mux.HandleFunc("GET /api/tasks/{id}/runs/{run_id}", r.handleGetRun)
		r.mux.HandleFunc("GET /api/runs/recent", r.handleRecentRuns)
		r.mux.HandleFunc("POST /api/runs/{id}/cancel", r.handleCancelRun)
		r.mux.HandleFunc("DELETE /api/runs/{id}", r.handleDeleteRun)
		r.mux.HandleFunc("GET /api/runs/{id}", r.handleGetRunByID)
		r.mux.HandleFunc("DELETE /api/runs", r.handleDeleteRuns)
		r.mux.HandleFunc("GET /api/runs", r.handleListRuns)
	}

	if r.notifications != nil {
		r.mux.HandleFunc("PUT /api/notifications/read-all", r.handleMarkAllNotificationsRead)
		r.mux.HandleFunc("PUT /api/notifications/read-tasks", r.handleMarkTaskNotificationsRead)
		r.mux.HandleFunc("GET /api/notifications", r.handleListNotifications)
		r.mux.HandleFunc("PUT /api/notifications/{id}/read", r.handleMarkNotificationRead)
		r.mux.HandleFunc("DELETE /api/notifications/{id}", r.handleDeleteNotification)
		r.mux.HandleFunc("DELETE /api/notifications", r.handleDeleteAllNotifications)
	}

	if r.pushTokens != nil {
		r.mux.HandleFunc("POST /api/push-tokens", r.handleRegisterPushToken)
		r.mux.HandleFunc("DELETE /api/push-tokens", r.handleUnregisterPushTokens)
	}

	if r.skills != nil {
		r.mux.HandleFunc("GET /api/skills", r.handleListSkills)
		r.mux.HandleFunc("GET /api/skills/search", r.handleSearchSkills)
		r.mux.HandleFunc("GET /api/skills/detail/{slug}", r.handleSkillDetail)
		r.mux.HandleFunc("POST /api/skills/install", r.handleInstallSkill)
		r.mux.HandleFunc("POST /api/skills/import", r.handleImportSkill)
		r.mux.HandleFunc("GET /api/skills/nodes/{id}/content", r.handleReadSkillContent)
		r.mux.HandleFunc("PUT /api/skills/nodes/{id}", r.handleUpdateSkill)
		r.mux.HandleFunc("DELETE /api/skills/{id}", r.handleUninstallSkill)
	}

	if r.tools != nil {
		r.mux.HandleFunc("GET /api/tools", r.handleListTools)
		r.mux.HandleFunc("GET /api/tools/{name}", r.handleGetTool)
		r.mux.HandleFunc("DELETE /api/tools/{name}", r.handleDeleteTool)
	}

	if r.configStore != nil {
		r.mux.HandleFunc("GET /api/settings", r.handleGetSettings)
		r.mux.HandleFunc("PUT /api/settings", r.handleUpdateSettings)
	}

	if r.ollama != nil {
		r.mux.HandleFunc("GET /api/ollama/status", r.handleOllamaStatus)
		r.mux.HandleFunc("GET /api/ollama/models", r.handleListOllamaModels)
		r.mux.HandleFunc("POST /api/ollama/pull", r.handlePullOllamaModel)
		r.mux.HandleFunc("DELETE /api/ollama/models/{name}", r.handleDeleteOllamaModel)
	}

	if r.db != nil {
		r.mux.HandleFunc("GET /api/usage/daily", r.handleDailyTokenStats)
		r.mux.HandleFunc("GET /api/audit/logs", r.handleListAuditLogs)
	}

	if r.web != nil {
		r.mux.Handle("GET /ws", r.web)
	}

	// Browser connector.
	r.mux.HandleFunc("GET /api/connectors/browser/status", r.handleBrowserStatus)
	r.mux.HandleFunc("POST /api/connectors/browser/enable", r.handleBrowserEnable)
	r.mux.HandleFunc("POST /api/connectors/browser/disable", r.handleBrowserDisable)

	// Connectors.
	r.mux.HandleFunc("GET /api/connectors", r.handleConnectorsList)
	r.mux.HandleFunc("GET /api/connectors/{name}/status", r.handleConnectorStatus)
	r.mux.HandleFunc("GET /api/connectors/{name}/auth/start", r.handleConnectorAuthStart)
	r.mux.HandleFunc("DELETE /api/connectors/{name}/auth", r.handleConnectorDisconnect)
	r.mux.HandleFunc("GET /api/connectors/{name}/settings", r.handleConnectorSettings)
	r.mux.HandleFunc("PUT /api/connectors/{name}/settings", r.handleConnectorSettingsUpdate)
	r.mux.HandleFunc("POST /api/connectors/{name}/settings/refresh", r.handleConnectorSettingsRefresh)

	if r.mcp != nil {
		r.mux.HandleFunc("GET /api/mcp/servers", r.handleListMCPServers)
		r.mux.HandleFunc("POST /api/mcp/servers", r.handleAddMCPServer)
		r.mux.HandleFunc("DELETE /api/mcp/servers/{name}", r.handleRemoveMCPServer)
		r.mux.HandleFunc("POST /api/mcp/servers/{name}/start", r.handleStartMCPServer)
		r.mux.HandleFunc("POST /api/mcp/servers/{name}/stop", r.handleStopMCPServer)
		r.mux.HandleFunc("GET /api/mcp/servers/{name}/tools", r.handleListMCPTools)
		r.mux.HandleFunc("POST /api/mcp/servers/{name}/tools/{tool}/test", r.handleTestMCPTool)
		r.mux.HandleFunc("PATCH /api/mcp/servers/{name}", r.handleUpdateMCPServer)
		r.mux.HandleFunc("PUT /api/mcp/servers/{name}/secrets", r.handleUpdateMCPSecrets)
	}

	// Internal endpoints (SaaS orchestrator communication).
	if r.internalSecret != "" {
		internal := internalAuth(r.internalSecret)
		if r.metricsRing != nil {
			r.mux.Handle("GET /api/internal/metrics", internal(http.HandlerFunc(r.handleMetrics)))
		}
		if r.drainManager != nil {
			r.mux.Handle("POST /api/internal/drain", internal(http.HandlerFunc(r.handleDrain)))
		}
		if r.dashboardDir != "" {
			r.mux.Handle("POST /api/internal/update-frontend", internal(http.HandlerFunc(r.handleUpdateFrontend)))
		}
	}

	// Serve dashboard static files (SPA with fallback to index.html).
	if r.dashboardFS != nil {
		fsys := r.dashboardFS
		fileServer := http.FileServerFS(fsys)
		r.mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/api/") || strings.HasPrefix(req.URL.Path, "/ws") {
				http.NotFound(w, req)
				return
			}
			name := strings.TrimPrefix(req.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			if f, err := fsys.Open(name); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, req)
				return
			}
			// SPA fallback: serve index.html.
			f, err := fsys.Open("index.html")
			if err != nil {
				http.NotFound(w, req)
				return
			}
			defer f.Close()
			stat, _ := f.Stat()
			content, ok := f.(io.ReadSeeker)
			if !ok {
				http.NotFound(w, req)
				return
			}
			http.ServeContent(w, req, "index.html", stat.ModTime(), content)
		})
	} else if r.dashboardDir != "" {
		diskFS := http.FileServer(http.Dir(r.dashboardDir))
		r.mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			p := filepath.Join(r.dashboardDir, filepath.Clean(req.URL.Path))
			if !strings.HasPrefix(req.URL.Path, "/api/") && !strings.HasPrefix(req.URL.Path, "/ws") {
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					diskFS.ServeHTTP(w, req)
					return
				}
			}
			http.ServeFile(w, req, filepath.Join(r.dashboardDir, "index.html"))
		})
	}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}
