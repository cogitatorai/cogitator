package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/worker"
)

type settingsResponse struct {
	Workspace workspaceResponse            `json:"workspace"`
	Models    modelsResponse              `json:"models"`
	Providers map[string]providerResponse `json:"providers"`
	Telegram  telegramSettingsResponse    `json:"telegram"`
	Security  securitySettingsResponse    `json:"security"`
	Server    serverSettingsResponse      `json:"server"`
	Memory    memorySettingsResponse      `json:"memory"`
}

type serverSettingsResponse struct {
	PublicURL string `json:"public_url"`
}

type workspaceResponse struct {
	Path string `json:"path"`
}

type securitySettingsResponse struct {
	AllowedDomains []string `json:"allowed_domains"`
}

type telegramSettingsResponse struct {
	Enabled        bool    `json:"enabled"`
	BotTokenSet    bool    `json:"bot_token_set"`
	AllowedChatIDs []int64 `json:"allowed_chat_ids"`
}

type modelsResponse struct {
	Standard modelResponse `json:"standard"`
	Cheap     modelResponse `json:"cheap"`
}

type modelResponse struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type providerResponse struct {
	APIKeySet bool `json:"api_key_set"`
}

type memorySettingsResponse struct {
	EmbeddingModel string `json:"embedding_model"`
}

type memorySettingsUpdate struct {
	EmbeddingModel *string `json:"embedding_model"`
}

type settingsUpdateRequest struct {
	Workspace *workspaceUpdate                 `json:"workspace"`
	Models    *modelsUpdateRequest             `json:"models"`
	Providers map[string]providerUpdateRequest `json:"providers"`
	Telegram  *telegramSettingsUpdate          `json:"telegram"`
	Security  *securitySettingsUpdate          `json:"security"`
	Server    *serverSettingsUpdate            `json:"server"`
	Memory    *memorySettingsUpdate            `json:"memory"`
}

type serverSettingsUpdate struct {
	PublicURL *string `json:"public_url"`
}

type workspaceUpdate struct {
	Path string `json:"path"`
}

type securitySettingsUpdate struct {
	AllowedDomains *[]string `json:"allowed_domains"`
}

type telegramSettingsUpdate struct {
	Enabled        *bool    `json:"enabled"`
	BotToken       string   `json:"bot_token"`
	AllowedChatIDs *[]int64 `json:"allowed_chat_ids"`
}

type modelsUpdateRequest struct {
	Standard *modelUpdateRequest `json:"standard"`
	Cheap     *modelUpdateRequest `json:"cheap"`
}

type modelUpdateRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type providerUpdateRequest struct {
	APIKey string `json:"api_key"`
}

// ProviderFactory builds a provider.Provider from a provider name and API key.
type ProviderFactory func(name, apiKey string) (provider.Provider, error)

func (r *Router) handleGetSettings(w http.ResponseWriter, req *http.Request) {
	if req != nil && !requireAdmin(w, req) {
		return
	}
	cfg := r.configStore.Get()

	providers := make(map[string]providerResponse)
	for name, pc := range cfg.Providers {
		providers[name] = providerResponse{APIKeySet: pc.APIKey != ""}
	}

	chatIDs := cfg.Channels.Telegram.AllowedChatIDs
	if chatIDs == nil {
		chatIDs = []int64{}
	}

	allowedDomains := cfg.Security.AllowedDomains
	if allowedDomains == nil {
		allowedDomains = []string{}
	}

	resp := settingsResponse{
		Workspace: workspaceResponse{
			Path: cfg.Workspace.Path,
		},
		Models: modelsResponse{
			Standard: modelResponse{
				Provider: cfg.Models.Standard.Provider,
				Model:    cfg.Models.Standard.Model,
			},
			Cheap: modelResponse{
				Provider: cfg.Models.Cheap.Provider,
				Model:    cfg.Models.Cheap.Model,
			},
		},
		Providers: providers,
		Telegram: telegramSettingsResponse{
			Enabled:        cfg.Channels.Telegram.Enabled,
			BotTokenSet:    cfg.Channels.Telegram.BotToken != "",
			AllowedChatIDs: chatIDs,
		},
		Security: securitySettingsResponse{
			AllowedDomains: allowedDomains,
		},
		Server: serverSettingsResponse{
			PublicURL: cfg.Server.PublicURL,
		},
		Memory: memorySettingsResponse{
			EmbeddingModel: cfg.Memory.EmbeddingModel,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleUpdateSettings(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}

	var body settingsUpdateRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	cfg := r.configStore.Get()
	oldEmbeddingModel := cfg.Memory.EmbeddingModel

	if body.Workspace != nil && body.Workspace.Path != "" {
		cfg.Workspace.Path = body.Workspace.Path
	}

	if body.Models != nil {
		if body.Models.Standard != nil {
			if body.Models.Standard.Provider != "" {
				cfg.Models.Standard.Provider = body.Models.Standard.Provider
			}
			if body.Models.Standard.Model != "" {
				cfg.Models.Standard.Model = body.Models.Standard.Model
			}
		}
		if body.Models.Cheap != nil {
			if body.Models.Cheap.Provider != "" {
				cfg.Models.Cheap.Provider = body.Models.Cheap.Provider
			}
			if body.Models.Cheap.Model != "" {
				cfg.Models.Cheap.Model = body.Models.Cheap.Model
			}
		}
	}

	for name, pc := range body.Providers {
		if pc.APIKey != "" {
			cfg.SetProviderAPIKey(name, pc.APIKey)
		}
	}

	restartTelegram := false
	if body.Telegram != nil {
		tg := body.Telegram
		if tg.Enabled != nil {
			cfg.Channels.Telegram.Enabled = *tg.Enabled
			restartTelegram = true
		}
		if tg.BotToken != "" {
			cfg.Channels.Telegram.BotToken = tg.BotToken
			restartTelegram = true
		}
		if tg.AllowedChatIDs != nil {
			cfg.Channels.Telegram.AllowedChatIDs = *tg.AllowedChatIDs
			restartTelegram = true
		}
	}

	if body.Security != nil && body.Security.AllowedDomains != nil {
		cfg.Security.AllowedDomains = *body.Security.AllowedDomains
		if r.domainSetter != nil {
			r.domainSetter.SetAllowedDomains(cfg.Security.AllowedDomains)
		}
	}

	if body.Memory != nil && body.Memory.EmbeddingModel != nil && *body.Memory.EmbeddingModel != "" {
		cfg.Memory.EmbeddingModel = *body.Memory.EmbeddingModel
	}

	if body.Server != nil && body.Server.PublicURL != nil {
		u := strings.TrimRight(*body.Server.PublicURL, "/")
		if u != "" {
			parsed, err := url.Parse(u)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				writeError(w, http.StatusBadRequest, "invalid public URL")
				return
			}
		}
		cfg.Server.PublicURL = u
	}

	cfg.ResolveDefaults()

	if err := r.configStore.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	// Hot-swap the provider on the agent if the standard model's provider is configured.
	stdProvider := cfg.Models.Standard.Provider
	stdKey := cfg.ProviderAPIKey(stdProvider)
	if r.agent != nil && r.providerFactory != nil && stdProvider != "" && (stdKey != "" || provider.IsKeyless(stdProvider)) {
		stdP, err := r.providerFactory(stdProvider, stdKey)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider: "+err.Error())
			return
		}
		r.agent.SetProvider(stdP, cfg.Models.Standard.Model)

		// Build the cheap provider (may differ from standard).
		cheapProvider := cfg.Models.Cheap.Provider
		cheapModel := cfg.Models.Cheap.Model
		cheapP := stdP // default: same as standard
		if cheapProvider != "" && cheapProvider != stdProvider {
			cheapKey := cfg.ProviderAPIKey(cheapProvider)
			if cheapKey != "" || provider.IsKeyless(cheapProvider) {
				if cp, err := r.providerFactory(cheapProvider, cheapKey); err == nil {
					cheapP = cp
				}
			}
		}
		if cheapModel == "" {
			cheapModel = cfg.Models.Standard.Model
		}
		if cheapP != stdP {
			r.agent.SetModelProvider(cheapModel, cheapP)
		}
		if r.retriever != nil {
			r.retriever.SetProvider(cheapP, cheapModel)
			r.retriever.SetStandardProvider(stdP, cfg.Models.Standard.Model)
		}
		if r.enricher != nil {
			r.enricher.SetProvider(cheapP, cheapModel)
		}
	}

	// Hot-swap embedding provider (reuses the standard provider's connection).
	if stdProvider != "" && (stdKey != "" || provider.IsKeyless(stdProvider)) {
		embModel := cfg.Memory.EmbeddingModel
		if embModel != "" {
			embP := provider.NewOpenAI(stdProvider, stdKey)
			if r.retriever != nil {
				r.retriever.SetEmbedder(embP, embModel)
			}
			if r.nodeEmbedder != nil {
				r.nodeEmbedder.SetEmbedder(embP, embModel)
			}

			// If the embedding model changed, purge old vectors and re-embed.
			if embModel != oldEmbeddingModel {
				if r.retriever != nil {
					r.retriever.InvalidateCache()
				}
				if r.memory != nil && r.nodeEmbedder != nil {
					ne := r.nodeEmbedder
					ms := r.memory
					go func() {
						if _, err := ms.DeleteAllEmbeddings(); err != nil {
							return
						}
						worker.RunBackfill(context.Background(), ms, ne, 50)
					}()
				}
			}
		}
	}

	if restartTelegram && r.telegram != nil {
		go r.telegram.Restart(context.Background())
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.SettingsChanged})
	}

	r.handleGetSettings(w, nil)
}
