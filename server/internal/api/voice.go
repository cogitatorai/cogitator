package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/voice"
)

const voiceChunkSize = 8 * 1024 // 8KB per TTS audio chunk

type voiceResponse struct {
	ThreadID      string `json:"thread_id"`
	MessageID     string `json:"message_id"`
	Transcription string `json:"transcription"`
}

func (r *Router) handleVoice(w http.ResponseWriter, req *http.Request) {
	// Require voice to be configured.
	if r.voiceRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "voice not configured")
		return
	}
	if r.configStore == nil {
		writeError(w, http.StatusServiceUnavailable, "voice not configured")
		return
	}
	cfg := r.configStore.Get()
	if cfg.Voice.STTProvider == "" || cfg.Voice.TTSProvider == "" {
		writeError(w, http.StatusServiceUnavailable, "voice not configured: STT and TTS providers required")
		return
	}

	// Enforce upload size limit.
	maxBytes := int64(cfg.Voice.MaxUploadBytes)
	if maxBytes <= 0 {
		maxBytes = 25 * 1024 * 1024 // 25MB default
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBytes+1<<20) // +1MB for form overhead

	if err := req.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse form: "+err.Error())
		return
	}

	// Validate thread_id XOR new_thread.
	threadID := req.FormValue("thread_id")
	newThread := req.FormValue("new_thread") == "true"

	if threadID != "" && newThread {
		writeError(w, http.StatusBadRequest, "provide either thread_id or new_thread, not both")
		return
	}
	if threadID == "" && !newThread {
		writeError(w, http.StatusBadRequest, "provide either thread_id or new_thread")
		return
	}

	// Read the audio form file.
	audioFile, audioHeader, err := req.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "audio file is required")
		return
	}
	defer audioFile.Close()
	audioData, err := io.ReadAll(io.LimitReader(audioFile, maxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read audio file")
		return
	}

	uid := userIDFromRequest(req)

	// For existing threads, verify the session exists and the user owns it.
	// If the session doesn't exist yet (e.g. mobile app generated a key but
	// no messages were sent), treat it as a valid key and let the agent
	// create the session on first message.
	if threadID != "" && r.sessions != nil {
		sess, err := r.sessions.Get(threadID, uid)
		if err == nil && sess != nil {
			// Session exists and user has access. Proceed.
		} else {
			// Session not found. This is OK: the agent's GetOrCreate will
			// create it when processing the chat request.
			slog.Debug("voice: session not found, will be created by agent", "thread_id", threadID)
		}
	}

	// Create a new session key for new threads.
	if newThread {
		threadID = "mobile:" + ulid.Make().String()
	}

	// Resolve STT provider and transcribe with timeout.
	sttProvider, err := r.voiceRegistry.STT(cfg.Voice.STTProvider)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STT provider unavailable: "+err.Error())
		return
	}

	sttTimeout := time.Duration(cfg.Voice.STTTimeoutSec) * time.Second
	if sttTimeout <= 0 {
		sttTimeout = 30 * time.Second
	}
	sttCtx, sttCancel := context.WithTimeout(req.Context(), sttTimeout)
	defer sttCancel()

	// Extract format from the uploaded filename (e.g. "recording.m4a" -> "m4a").
	audioFormat := "m4a"
	if audioHeader != nil && audioHeader.Filename != "" {
		if ext := filepath.Ext(audioHeader.Filename); ext != "" {
			audioFormat = strings.TrimPrefix(ext, ".")
		}
	}
	transcription, err := sttProvider.Transcribe(sttCtx, audioData, audioFormat)
	if err != nil {
		if errors.Is(err, voice.ErrTranscriptionEmpty) {
			// Empty transcription: publish error event, return OK with empty transcription.
			r.publishVoiceError(uid, threadID, "transcription returned empty text")
			writeJSON(w, http.StatusOK, voiceResponse{
				ThreadID:      threadID,
				Transcription: "",
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "transcription failed: "+err.Error())
		return
	}
	if transcription == "" {
		r.publishVoiceError(uid, threadID, "transcription returned empty text")
		writeJSON(w, http.StatusOK, voiceResponse{
			ThreadID:      threadID,
			Transcription: "",
		})
		return
	}

	// Resolve user profile for the agent request.
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

	// Build the agent ChatRequest following the handleChat pattern.
	chatReq := agent.ChatRequest{
		SessionKey:       threadID,
		Channel:          "mobile",
		ChatID:           threadID,
		UserID:           uid,
		UserName:         userName,
		UserRole:         userRole,
		Message:          transcription,
		ProfileOverrides: profileOverrides,
	}

	// Generate a stable message ID for correlation with the async TTS response.
	msgID := ulid.Make().String()

	// Return immediately with thread_id, message_id, and transcription.
	writeJSON(w, http.StatusOK, voiceResponse{
		ThreadID:      threadID,
		MessageID:     msgID,
		Transcription: transcription,
	})

	// Async: run LLM + TTS in the background.
	go r.processVoiceResponse(uid, threadID, msgID, chatReq)
}

// processVoiceResponse runs the LLM chat and streams TTS audio chunks over the event bus.
func (r *Router) processVoiceResponse(userID, threadID, msgID string, chatReq agent.ChatRequest) {
	// Subscribe to VoiceCancel so we can abort if the client cancels this message.
	cancelCh := r.eventBus.Subscribe(bus.VoiceCancel)
	defer r.eventBus.Unsubscribe(cancelCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for cancel events matching our message ID.
	go func() {
		for evt := range cancelCh {
			uid, _ := evt.Payload["user_id"].(string)
			mid, _ := evt.Payload["message_id"].(string)
			if uid == userID && mid == msgID {
				cancel()
				return
			}
		}
	}()

	// Call the agent.
	agentResp, err := r.agent.Chat(ctx, chatReq)
	if err != nil {
		slog.Warn("voice: agent chat failed", "thread_id", threadID, "message_id", msgID, "error", err)
		r.publishVoiceError(userID, threadID, "agent error: "+err.Error())
		return
	}

	// Resolve TTS provider.
	cfg := r.configStore.Get()
	ttsProvider, err := r.voiceRegistry.TTS(cfg.Voice.TTSProvider)
	if err != nil {
		slog.Warn("voice: TTS provider unavailable", "thread_id", threadID, "error", err)
		r.publishVoiceError(userID, threadID, "TTS provider unavailable: "+err.Error())
		return
	}

	// Publish VoiceAudioStart.
	r.eventBus.Publish(bus.Event{
		Type: bus.VoiceAudioStart,
		Payload: map[string]any{
			"user_id":    userID,
			"thread_id":  threadID,
			"message_id": msgID,
			"format":     cfg.Voice.AudioFormat,
		},
	})

	// Synthesize and stream audio chunks.
	ttsVoice := cfg.Voice.TTSVoice
	audioReader, err := ttsProvider.Synthesize(agentResp.Content, ttsVoice)
	if err != nil {
		slog.Warn("voice: synthesis failed", "thread_id", threadID, "error", err)
		r.publishVoiceError(userID, threadID, "synthesis failed: "+err.Error())
		return
	}
	defer audioReader.Close()

	buf := make([]byte, voiceChunkSize)
	seq := 0
	for {
		if ctx.Err() != nil {
			slog.Info("voice: cancelled during TTS streaming", "thread_id", threadID, "message_id", msgID)
			r.eventBus.Publish(bus.Event{
				Type: bus.VoiceAudioEnd,
				Payload: map[string]any{
					"user_id":    userID,
					"thread_id":  threadID,
					"message_id": msgID,
					"chunks":     seq,
				},
			})
			return
		}

		n, readErr := audioReader.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			r.eventBus.Publish(bus.Event{
				Type: bus.VoiceAudioChunk,
				Payload: map[string]any{
					"user_id":    userID,
					"thread_id":  threadID,
					"message_id": msgID,
					"seq":        seq,
					"data":       encoded,
				},
			})
			seq++
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			slog.Warn("voice: error reading TTS audio", "thread_id", threadID, "error", readErr)
			r.publishVoiceError(userID, threadID, fmt.Sprintf("TTS read error: %v", readErr))
			return
		}
	}

	// Publish VoiceAudioEnd.
	r.eventBus.Publish(bus.Event{
		Type: bus.VoiceAudioEnd,
		Payload: map[string]any{
			"user_id":    userID,
			"thread_id":  threadID,
			"message_id": msgID,
			"chunks":     seq,
		},
	})
}

// publishVoiceError publishes a VoiceError event on the bus.
func (r *Router) publishVoiceError(userID, threadID, reason string) {
	if r.eventBus == nil {
		return
	}
	r.eventBus.Publish(bus.Event{
		Type: bus.VoiceError,
		Payload: map[string]any{
			"user_id":   userID,
			"thread_id": threadID,
			"error":     reason,
		},
	})
}
