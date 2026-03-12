package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/budget"
	"github.com/cogitatorai/cogitator/server/internal/fileproc"
)

type chatRequest struct {
	Message    string `json:"message"`
	SessionKey string `json:"session_key"`
	Channel    string `json:"channel"`
	ChatID     string `json:"chat_id"`
	Private    bool   `json:"private,omitempty"`
}

type chatResponse struct {
	Content    string `json:"content"`
	SessionKey string `json:"session_key"`
	ToolsUsed  any    `json:"tools_used,omitempty"`
}

func (r *Router) handleChat(w http.ResponseWriter, req *http.Request) {
	var body chatRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Default session key from channel and chat ID
	if body.SessionKey == "" {
		if body.Channel == "" {
			body.Channel = "web"
		}
		if body.ChatID == "" {
			body.ChatID = "default"
		}
		body.SessionKey = body.Channel + ":" + body.ChatID
	}

	uid := userIDFromRequest(req)

	var profileOverrides string
	var userName string
	var userRole string
	if r.users != nil && uid != "" {
		if u, err := r.users.Get(uid); err == nil {
			profileOverrides = u.ProfileOverrides
			userName = u.Name
			userRole = string(u.Role)
		}
	}

	resp, err := r.agent.Chat(req.Context(), agent.ChatRequest{
		SessionKey:       body.SessionKey,
		Channel:          body.Channel,
		ChatID:           body.ChatID,
		UserID:           uid,
		UserName:         userName,
		UserRole:         userRole,
		Private:          body.Private,
		Message:          body.Message,
		ProfileOverrides: profileOverrides,
	})
	if err != nil {
		writeChatError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{
		Content:    resp.Content,
		SessionKey: body.SessionKey,
		ToolsUsed:  resp.ToolsUsed,
	})
}

func (r *Router) handleChatWithFile(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(fileproc.MaxFileSize + 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse form: "+err.Error())
		return
	}

	message := req.FormValue("message")
	sessionKey := req.FormValue("session_key")
	channel := req.FormValue("channel")
	chatID := req.FormValue("chat_id")
	private := req.FormValue("private") == "true"

	if channel == "" {
		channel = "web"
	}
	if sessionKey == "" {
		if chatID == "" {
			chatID = "default"
		}
		sessionKey = channel + ":" + chatID
	}

	var attachments []fileproc.ContentBlock

	file, header, err := req.FormFile("file")
	if err == nil {
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, fileproc.MaxFileSize+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read file")
			return
		}
		blocks, err := fileproc.Process(header.Filename, data)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		attachments = blocks
	}

	if message == "" && len(attachments) == 0 {
		writeError(w, http.StatusBadRequest, "message or file is required")
		return
	}

	uid := userIDFromRequest(req)

	var profileOverrides string
	var userName string
	var userRole string
	if r.users != nil && uid != "" {
		if u, err := r.users.Get(uid); err == nil {
			profileOverrides = u.ProfileOverrides
			userName = u.Name
			userRole = string(u.Role)
		}
	}

	resp, err := r.agent.Chat(req.Context(), agent.ChatRequest{
		SessionKey:       sessionKey,
		Channel:          channel,
		ChatID:           chatID,
		UserID:           uid,
		UserName:         userName,
		UserRole:         userRole,
		Private:          private,
		Message:          message,
		Attachments:      attachments,
		ProfileOverrides: profileOverrides,
	})
	if err != nil {
		writeChatError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{
		Content:    resp.Content,
		SessionKey: sessionKey,
		ToolsUsed:  resp.ToolsUsed,
	})
}

// writeChatError maps agent errors to appropriate HTTP status codes.
func writeChatError(w http.ResponseWriter, err error) {
	if errors.Is(err, budget.ErrDailyBudgetExceeded) || errors.Is(err, budget.ErrRateLimited) {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "agent error: "+err.Error())
}
