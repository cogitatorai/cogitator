package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

func TestHandleChatWithFile(t *testing.T) {
	router := setupTestRouter(t, provider.Response{Content: "I see your file"})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("message", "check this file")
	writer.WriteField("session_key", "web:test-upload")
	writer.WriteField("chat_id", "test-upload")

	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("Hello from the file"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/chat/message", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp chatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Content != "I see your file" {
		t.Errorf("expected 'I see your file', got %q", resp.Content)
	}
	if resp.SessionKey != "web:test-upload" {
		t.Errorf("expected session key 'web:test-upload', got %q", resp.SessionKey)
	}
}

func TestHandleChatWithFileOnly(t *testing.T) {
	router := setupTestRouter(t, provider.Response{Content: "Got the image"})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("message", "")
	writer.WriteField("session_key", "web:file-only")
	writer.WriteField("chat_id", "file-only")

	part, _ := writer.CreateFormFile("file", "data.csv")
	part.Write([]byte("a,b,c\n1,2,3"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/chat/message", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleChatWithFileUnsupported(t *testing.T) {
	router := setupTestRouter(t)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("message", "check this")
	writer.WriteField("session_key", "web:bad-file")
	writer.WriteField("chat_id", "bad-file")

	part, _ := writer.CreateFormFile("file", "archive.zip")
	part.Write([]byte("PK\x03\x04"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/chat/message", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatWithFileNoContent(t *testing.T) {
	router := setupTestRouter(t)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("message", "")
	writer.WriteField("session_key", "web:empty")
	writer.WriteField("chat_id", "empty")
	writer.Close()

	req := httptest.NewRequest("POST", "/api/chat/message", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
