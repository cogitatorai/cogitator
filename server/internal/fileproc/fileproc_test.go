package fileproc

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Minimal valid PNG: 1x1 pixel, RGBA.
var minimalPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC,
	0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
	0x44, 0xAE, 0x42, 0x60, 0x82,
}

// JPEG magic bytes (SOI marker + JFIF APP0 header start).
var minimalJPEG = []byte{
	0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46,
	0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
	0x00, 0x01, 0x00, 0x00,
}

func TestProcessPlainText(t *testing.T) {
	blocks, err := Process("notes.txt", []byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Type != "text" {
		t.Errorf("expected type 'text', got %q", b.Type)
	}
	if !strings.Contains(b.Text, "the user attached the file \"notes.txt\"") {
		t.Errorf("expected filename in text, got %q", b.Text)
	}
	if !strings.Contains(b.Text, "hello world") {
		t.Errorf("expected content in text, got %q", b.Text)
	}
}

func TestProcessMarkdown(t *testing.T) {
	blocks, err := Process("readme.md", []byte("# Title\n\nSome text."))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Type != "text" {
		t.Errorf("expected type 'text', got %q", b.Type)
	}
	if !strings.Contains(b.Text, "the user attached the file \"readme.md\"") {
		t.Errorf("expected filename in text, got %q", b.Text)
	}
	if !strings.Contains(b.Text, "# Title") {
		t.Errorf("expected markdown content in text, got %q", b.Text)
	}
}

func TestProcessCSV(t *testing.T) {
	blocks, err := Process("data.csv", []byte("name,age\nAlice,30\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Type != "text" {
		t.Errorf("expected type 'text', got %q", b.Type)
	}
	if !strings.Contains(b.Text, "the user attached the file \"data.csv\"") {
		t.Errorf("expected filename in text, got %q", b.Text)
	}
	if !strings.Contains(b.Text, "name,age") {
		t.Errorf("expected CSV content in text, got %q", b.Text)
	}
}

func TestProcessImage(t *testing.T) {
	blocks, err := Process("photo.png", minimalPNG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Type != "image_url" {
		t.Errorf("expected type 'image_url', got %q", b.Type)
	}
	if b.ImageURL == nil {
		t.Fatal("expected ImageURL to be set")
	}
	if !strings.HasPrefix(b.ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("expected PNG data URI, got %q", b.ImageURL.URL)
	}
}

func TestProcessJPEG(t *testing.T) {
	blocks, err := Process("photo.jpg", minimalJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Type != "image_url" {
		t.Errorf("expected type 'image_url', got %q", b.Type)
	}
	if b.ImageURL == nil {
		t.Fatal("expected ImageURL to be set")
	}
	if !strings.Contains(b.ImageURL.URL, "image/jpeg") {
		t.Errorf("expected JPEG MIME in data URI, got %q", b.ImageURL.URL)
	}
}

func TestProcessEmptyFile(t *testing.T) {
	_, err := Process("test.txt", []byte{})
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if err.Error() != "file is empty" {
		t.Errorf("expected 'file is empty' error, got %q", err.Error())
	}
}

func TestProcessEmptyFilename(t *testing.T) {
	_, err := Process("", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
	if err.Error() != "filename is required" {
		t.Errorf("expected 'filename is required' error, got %q", err.Error())
	}
}

func TestProcessNoExtension(t *testing.T) {
	_, err := Process("README", []byte("data"))
	if err == nil {
		t.Fatal("expected error for filename without extension")
	}
	if err.Error() != "file has no extension" {
		t.Errorf("expected 'file has no extension' error, got %q", err.Error())
	}
}

func TestProcessUnsupported(t *testing.T) {
	_, err := Process("archive.zip", []byte("PK\x03\x04"))
	if err == nil {
		t.Fatal("expected error for unsupported file")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error, got %q", err.Error())
	}
}

func TestProcessTooLarge(t *testing.T) {
	data := make([]byte, MaxFileSize+1)
	_, err := Process("huge.txt", data)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' in error, got %q", err.Error())
	}
}

func createTestDocx(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	ct, _ := zw.Create("[Content_Types].xml")
	ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`))

	rels, _ := zw.Create("_rels/.rels")
	rels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`))

	wrels, _ := zw.Create("word/_rels/document.xml.rels")
	wrels.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`))

	doc, _ := zw.Create("word/document.xml")
	doc.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>%s</w:t></w:r></w:p>
  </w:body>
</w:document>`, text)))

	zw.Close()
	return buf.Bytes()
}

func TestProcessPDF(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.pdf")
	if err != nil {
		t.Skip("testdata/sample.pdf not found")
	}
	blocks, err := Process("report.pdf", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatal("expected single text block from PDF")
	}
	if !strings.Contains(blocks[0].Text, "report.pdf") {
		t.Fatal("expected filename in output")
	}
	if !strings.Contains(blocks[0].Text, "Hello") {
		t.Fatalf("expected extracted text, got: %s", blocks[0].Text)
	}
}

func TestProcessDocx(t *testing.T) {
	data := createTestDocx(t, "Hello DOCX")
	blocks, err := Process("document.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatal("expected single text block from DOCX")
	}
	if !strings.Contains(blocks[0].Text, "document.docx") {
		t.Fatal("expected filename in output")
	}
	if !strings.Contains(blocks[0].Text, "Hello DOCX") {
		t.Fatalf("expected extracted text, got: %s", blocks[0].Text)
	}
}
