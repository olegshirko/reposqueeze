package clipboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/olegshirko/reposqueeze/internal/domain/gateway"
)

// TextClipboard works without the clipsync helper, using only tools every
// Mac has (pbcopy, pbpaste, plutil). It sends and receives plain text in the
// helper's payload format, so it interoperates with Macs that have the
// helper; received images are saved to a file. It cannot watch for ⌘C ⌘C.
type TextClipboard struct {
	// Paste and Copy read and write the clipboard text (pbpaste / pbcopy).
	Paste func() ([]byte, error)
	Copy  func([]byte) error
	// SaveDir is where received images go (default ~/Downloads).
	SaveDir string
}

var _ gateway.Clipboard = (*TextClipboard)(nil)

// NewTextClipboard uses pbcopy/pbpaste.
func NewTextClipboard() *TextClipboard {
	home, _ := os.UserHomeDir()
	utf8 := append(os.Environ(), "LANG=en_US.UTF-8", "LC_CTYPE=UTF-8")
	return &TextClipboard{
		Paste: func() ([]byte, error) {
			cmd := exec.Command("/usr/bin/pbpaste")
			cmd.Env = utf8
			return cmd.Output()
		},
		Copy: func(b []byte) error {
			cmd := exec.Command("/usr/bin/pbcopy")
			cmd.Env = utf8
			cmd.Stdin = bytes.NewReader(b)
			return cmd.Run()
		},
		SaveDir: filepath.Join(home, "Downloads"),
	}
}

// Default returns the clipsync helper when it is installed, otherwise the
// text-only clipboard.
func Default() gateway.Clipboard {
	bin := DefaultBin()
	if _, err := os.Stat(bin); err == nil {
		return NewClipsync(bin)
	}
	return NewTextClipboard()
}

const textType = "public.utf8-plain-text"

// Export packs the clipboard text as a one-item payload.
func (c *TextClipboard) Export() ([]byte, string, error) {
	text, err := c.Paste()
	if err != nil {
		return nil, "", fmt.Errorf("pbpaste: %w", err)
	}
	if len(text) == 0 {
		return nil, "", errors.New("the clipboard has no text (without the clipsync helper only text can be sent)")
	}
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\"><dict>")
	b.WriteString("<key>version</key><integer>1</integer>")
	b.WriteString("<key>files</key><dict/>")
	b.WriteString("<key>items</key><array><dict><key>" + textType + "</key><data>")
	b.WriteString(base64.StdEncoding.EncodeToString(text))
	b.WriteString("</data></dict></array></dict></plist>\n")
	return b.Bytes(), fmt.Sprintf("text, %d chars", len([]rune(string(text)))), nil
}

// Import puts the text of the payload into the clipboard; an image without
// text is saved to SaveDir and its path is copied instead.
func (c *TextClipboard) Import(payload []byte) (string, error) {
	items, err := parsePayload(payload)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if text, ok := item[textType]; ok {
			if err := c.Copy(text); err != nil {
				return "", fmt.Errorf("pbcopy: %w", err)
			}
			return fmt.Sprintf("text, %d chars", len([]rune(string(text)))), nil
		}
	}
	for _, item := range items {
		for typ, ext := range map[string]string{"public.png": "png", "public.jpeg": "jpg", "public.tiff": "tiff"} {
			if img, ok := item[typ]; ok {
				if err := os.MkdirAll(c.SaveDir, 0o755); err != nil {
					return "", err
				}
				path := filepath.Join(c.SaveDir, "clipboard-"+time.Now().Format("20060102-150405")+"."+ext)
				if err := os.WriteFile(path, img, 0o644); err != nil {
					return "", err
				}
				if err := c.Copy([]byte(path)); err != nil {
					return "", err
				}
				return "image saved to " + path + " (path copied)", nil
			}
		}
	}
	return "", errors.New("received clipboard has no text or image; files need the clipsync helper on this Mac")
}

// parsePayload reads the items of a clipsync payload (binary or XML plist)
// by converting it to XML with plutil.
func parsePayload(payload []byte) ([]map[string][]byte, error) {
	cmd := exec.CommandContext(context.Background(), "/usr/bin/plutil", "-convert", "xml1", "-o", "-", "-")
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("не разобрать полезную нагрузку")
	}
	return parseItemsXML(out)
}

// parseItemsXML extracts items: [{type: data}] from an XML plist.
func parseItemsXML(data []byte) ([]map[string][]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		items    []map[string][]byte
		depth    int // dict/array nesting
		inItems  bool
		itemsAt  int
		cur      map[string][]byte
		lastKey  string
		readText = func() (string, error) {
			var sb strings.Builder
			for {
				tok, err := dec.Token()
				if err != nil {
					return "", err
				}
				switch t := tok.(type) {
				case xml.CharData:
					sb.Write(t)
				case xml.EndElement:
					return sb.String(), nil
				}
			}
		}
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("не разобрать полезную нагрузку")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict", "array":
				depth++
				if inItems && t.Name.Local == "dict" && depth == itemsAt+1 {
					cur = map[string][]byte{}
				}
				if !inItems && t.Name.Local == "array" && lastKey == "items" && depth == 2 {
					inItems, itemsAt = true, depth
				}
			case "key":
				k, err := readText()
				if err != nil {
					return nil, err
				}
				lastKey = k
			case "data":
				s, err := readText()
				if err != nil {
					return nil, err
				}
				if cur != nil && depth == itemsAt+1 {
					b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
					if err != nil {
						return nil, errors.New("не разобрать полезную нагрузку")
					}
					cur[lastKey] = b
				}
			}
		case xml.EndElement:
			if t.Name.Local == "dict" || t.Name.Local == "array" {
				if inItems && t.Name.Local == "dict" && depth == itemsAt+1 && cur != nil {
					items = append(items, cur)
					cur = nil
				}
				if inItems && t.Name.Local == "array" && depth == itemsAt {
					inItems = false
				}
				depth--
			}
		}
	}
	if len(items) == 0 {
		return nil, errors.New("не разобрать полезную нагрузку")
	}
	return items, nil
}

// WatchDoubleCopy needs the helper.
func (c *TextClipboard) WatchDoubleCopy(ctx context.Context, window time.Duration, onDouble func()) error {
	return errors.New("watching for ⌘C ⌘C needs the clipsync helper (make clipsync); clip push and clip pull work without it")
}
