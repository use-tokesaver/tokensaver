package source

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

type Kind string

const (
	HTML    Kind = "html"
	PDF     Kind = "pdf"
	DOCX    Kind = "docx"
	XLSX    Kind = "xlsx"
	PPTX    Kind = "pptx"
	JSON    Kind = "json"
	Diff    Kind = "diff"
	LogFile Kind = "logfile"
	ZIP     Kind = "zip"
	Dir     Kind = "dir"
	Text    Kind = "text"
	Unknown Kind = ""
)

var extKinds = map[string]Kind{
	".html": HTML, ".htm": HTML, ".xhtml": HTML,
	".pdf":  PDF,
	".docx": DOCX, ".xlsx": XLSX, ".xlsm": XLSX, ".pptx": PPTX,
	".json": JSON, ".jsonl": JSON, ".ndjson": JSON, ".geojson": JSON,
	".diff": Diff, ".patch": Diff,
	".log":  LogFile,
	".zip":  ZIP,
}

// Detect decides what s is. Magic bytes win over headers (servers often say
// application/octet-stream), headers over file extensions, and content sniffing
// is the last resort.
func Detect(s *Source) Kind {
	d := s.Data
	switch {
	case bytes.HasPrefix(d, []byte("%PDF-")):
		return PDF
	case bytes.HasPrefix(d, []byte("PK\x03\x04")):
		if k := officeKind(d); k != Unknown {
			return k
		}
	}
	switch ct := s.ContentType; {
	case ct == "text/html" || ct == "application/xhtml+xml":
		return HTML
	case ct == "application/json" || strings.HasSuffix(ct, "+json") || ct == "application/x-ndjson":
		return JSON
	case ct == "application/pdf":
		return PDF
	}
	if k, ok := extKinds[strings.ToLower(path.Ext(s.Name()))]; ok {
		return k
	}
	head := bytes.ToLower(bytes.TrimSpace(d[:min(len(d), 2048)]))
	switch {
	case bytes.HasPrefix(head, []byte("<!doctype html")) || bytes.HasPrefix(head, []byte("<html")) ||
		bytes.Contains(head, []byte("<head")) || bytes.Contains(head, []byte("<body")):
		return HTML
	case (bytes.HasPrefix(head, []byte("{")) || bytes.HasPrefix(head, []byte("["))) && json.Valid(bytes.TrimSpace(d)):
		return JSON
	case bytes.HasPrefix(head, []byte("diff --git ")) ||
		(bytes.Contains(head, []byte("--- a/")) && bytes.Contains(head, []byte("+++ b/"))):
		return Diff
	}
	if looksText(d) {
		return Text
	}
	return Unknown
}

func officeKind(d []byte) Kind {
	zr, err := zip.NewReader(bytes.NewReader(d), int64(len(d)))
	if err != nil {
		return Unknown
	}
	for _, f := range zr.File {
		switch f.Name {
		case "word/document.xml":
			return DOCX
		case "xl/workbook.xml":
			return XLSX
		case "ppt/presentation.xml":
			return PPTX
		}
	}
	return Unknown
}

// looksText: valid UTF-8 (a rune cut at the sniff boundary is fine) with no NULs,
// or a declared/guessable 8-bit text encoding.
func looksText(d []byte) bool {
	sample := d[:min(len(d), 8192)]
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	for i := 0; i < 3 && len(sample) > 0 && !utf8.Valid(sample); i++ {
		sample = sample[:len(sample)-1]
	}
	return utf8.Valid(sample) || len(d) > 0 && !bytes.ContainsAny(sample, "\x01\x02\x03\x04\x05\x06\x07\x08\x0e\x0f")
}

// UTF8 returns s's content as UTF-8 text, converting from the declared or
// detected charset (e.g. windows-1252, Shift_JIS) when needed.
func (s *Source) UTF8() string {
	if utf8.Valid(s.Data) {
		return strings.TrimPrefix(string(s.Data), "\uFEFF")
	}
	ct := s.ContentType
	if s.Charset != "" {
		ct += "; charset=" + s.Charset
	}
	enc, _, _ := charset.DetermineEncoding(s.Data, ct)
	out, err := enc.NewDecoder().Bytes(s.Data)
	if err != nil {
		return strings.ToValidUTF8(string(s.Data), "�")
	}
	return string(out)
}
