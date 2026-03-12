package fileproc

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	pdf "github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
)

// MaxFileSize is the maximum allowed file size (10MB).
const MaxFileSize = 10 << 20

// ContentBlock represents a single content block for LLM consumption.
type ContentBlock struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds a data URI for an image content block.
type ImageURL struct {
	URL string `json:"url"`
}

// extMIME maps file extensions to MIME types for fallback detection.
var extMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// textExts lists extensions treated as plain text.
var textExts = map[string]bool{
	".txt": true,
	".md":  true,
	".csv": true,
}

// imageExts lists extensions treated as images.
var imageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
}

// Process takes a filename and its raw bytes, detects the file type, and
// returns the appropriate content blocks for LLM consumption.
func Process(filename string, data []byte) ([]ContentBlock, error) {
	if filename == "" {
		return nil, fmt.Errorf("filename is required")
	}
	if filepath.Ext(filename) == "" {
		return nil, fmt.Errorf("file has no extension")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes exceeds maximum of %d bytes", len(data), MaxFileSize)
	}

	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".pdf" {
		return processPDF(filename, data)
	}

	if ext == ".docx" {
		return processDocx(filename, data)
	}

	if textExts[ext] {
		return processText(filename, data), nil
	}

	if imageExts[ext] {
		return processImage(filename, data, ext)
	}

	return nil, fmt.Errorf("unsupported file format: %s", ext)
}

func processText(filename string, data []byte) []ContentBlock {
	content := fmt.Sprintf("[System: the user attached the file \"%s\". Its full content has been extracted and is shown below.]\n\n%s", filename, string(data))
	return []ContentBlock{
		{
			Type: "text",
			Text: content,
		},
	}
}

func processImage(_ string, data []byte, ext string) ([]ContentBlock, error) {
	mime := detectMIME(data, ext)
	encoded := base64.StdEncoding.EncodeToString(data)
	uri := fmt.Sprintf("data:%s;base64,%s", mime, encoded)

	return []ContentBlock{
		{
			Type: "image_url",
			ImageURL: &ImageURL{
				URL: uri,
			},
		},
	}, nil
}

func processPDF(filename string, data []byte) ([]ContentBlock, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF: %w", err)
	}

	var buf strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}

	extracted := strings.TrimSpace(buf.String())
	if extracted == "" {
		return nil, fmt.Errorf("no text could be extracted from PDF")
	}

	return []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("[System: the user attached the file \"%s\". Its full content has been extracted and is shown below.]\n\n%s", filename, extracted),
	}}, nil
}

func processDocx(filename string, data []byte) ([]ContentBlock, error) {
	tmpFile, err := os.CreateTemp("", "cogitator-docx-*.docx")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close() // Close before reading so the docx library can open it

	r, err := docx.ReadDocxFile(tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to read DOCX: %w", err)
	}
	defer r.Close()

	content := r.Editable().GetContent()
	content = stripXML(content)
	content = strings.TrimSpace(content)

	if content == "" {
		return nil, fmt.Errorf("no text could be extracted from DOCX")
	}

	return []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("[System: the user attached the file \"%s\". Its full content has been extracted and is shown below.]\n\n%s", filename, content),
	}}, nil
}

func stripXML(s string) string {
	var buf strings.Builder
	inTag := false
	prevSpace := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			if !prevSpace {
				buf.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		if !inTag {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				if !prevSpace {
					buf.WriteRune(' ')
					prevSpace = true
				}
			} else {
				buf.WriteRune(r)
				prevSpace = false
			}
		}
	}
	return buf.String()
}

func detectMIME(data []byte, ext string) string {
	detected := http.DetectContentType(data)
	if detected != "application/octet-stream" {
		return detected
	}
	if m, ok := extMIME[ext]; ok {
		return m
	}
	return detected
}
