package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	maxAttachmentCount     = 8
	maxTextAttachmentBytes = 12000
)

type InputAttachment struct {
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind,omitempty"`
	MIME string `json:"mime,omitempty"`
}

type UserInput struct {
	Text        string            `json:"input"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
	Isolated    bool              `json:"isolated,omitempty"`
}

type normalizedInput struct {
	Text  string
	Parts []sdk.MessagePart
}

func (rt *Runtime) normalizeUserInput(ctx context.Context, raw UserInput) (normalizedInput, error) {
	text := strings.TrimSpace(raw.Text)
	attachments := raw.Attachments
	if len(attachments) == 0 && looksLikeFilePath(text) {
		attachments = []InputAttachment{{Path: text}}
		text = ""
	}

	if len(attachments) > maxAttachmentCount {
		attachments = attachments[:maxAttachmentCount]
	}

	var notes []string
	parts := make([]sdk.MessagePart, 0, len(attachments))
	for _, att := range attachments {
		info, note := rt.processAttachment(ctx, att)
		if note != "" {
			notes = append(notes, note)
		}
		if len(info) > 0 {
			parts = append(parts, sdk.MessagePart{Type: "attachment", Input: info})
		}
	}

	if len(notes) > 0 {
		block := strings.Join(notes, "\n\n")
		if text == "" {
			text = block
		} else {
			text = text + "\n\n" + block
		}
	}

	return normalizedInput{Text: strings.TrimSpace(text), Parts: parts}, nil
}

func (rt *Runtime) processAttachment(ctx context.Context, att InputAttachment) (map[string]any, string) {
	info := map[string]any{}
	name := strings.TrimSpace(att.Name)
	if name != "" {
		info["name"] = name
	}
	if att.Kind != "" {
		info["kind"] = att.Kind
	}
	if att.MIME != "" {
		info["mime"] = att.MIME
	}

	if att.URL != "" {
		info["url"] = att.URL
		label := name
		if label == "" {
			label = att.URL
		}
		return info, fmt.Sprintf("Attachment (url): %s", label)
	}

	path := strings.TrimSpace(att.Path)
	if path == "" {
		return info, ""
	}
	path = expandUserPath(path)
	info["path"] = path
	if name == "" {
		name = filepath.Base(path)
		info["name"] = name
	}

	stat, err := os.Stat(path)
	if err != nil {
		info["error"] = err.Error()
		return info, fmt.Sprintf("Attachment error: %s (%v)", path, err)
	}
	if stat.IsDir() {
		info["error"] = "path is a directory"
		return info, fmt.Sprintf("Attachment error: %s is a directory", path)
	}
	info["size_bytes"] = stat.Size()

	kind := strings.TrimSpace(att.Kind)
	if kind == "" {
		kind = detectAttachmentKind(path)
		info["kind"] = kind
	}

	if kind == "text" {
		content, err := readTextAttachment(path)
		if err != nil {
			info["error"] = err.Error()
			return info, fmt.Sprintf("Attachment error: %s (%v)", path, err)
		}
		info["text"] = content
		return info, fmt.Sprintf("Attachment (text): %s\n%s", path, content)
	}
	return info, fmt.Sprintf("Attachment (%s): %s", kind, path)
}

func looksLikeFilePath(text string) bool {
	if text == "" {
		return false
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "\n") || strings.Contains(trimmed, " ") {
		return false
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "~") || strings.Contains(trimmed, string(os.PathSeparator)) {
		_, err := os.Stat(expandUserPath(trimmed))
		return err == nil
	}
	return false
}

func expandUserPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func detectAttachmentKind(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff":
		return "image"
	case ".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg":
		return "audio"
	case ".mp4", ".mov", ".mkv", ".avi", ".webm":
		return "video"
	case ".txt", ".md", ".json", ".yaml", ".yml", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".rb", ".php", ".html", ".css", ".toml":
		return "text"
	default:
		return "file"
	}
}

func readTextAttachment(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	limited := io.LimitReader(f, maxTextAttachmentBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("attachment is not valid UTF-8 text")
	}
	return strings.TrimSpace(string(data)), nil
}
