package clipboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeText(t *testing.T, content string) (*TextClipboard, *[]byte) {
	var copied []byte
	return &TextClipboard{
		Paste:   func() ([]byte, error) { return []byte(content), nil },
		Copy:    func(b []byte) error { copied = append([]byte(nil), b...); return nil },
		SaveDir: t.TempDir(),
	}, &copied
}

func toBinary(t *testing.T, xmlPlist []byte) []byte {
	cmd := exec.Command("/usr/bin/plutil", "-convert", "binary1", "-o", "-", "-")
	cmd.Stdin = bytes.NewReader(xmlPlist)
	out, err := cmd.Output()
	require.NoError(t, err)
	return out
}

func TestTextClipboard_RoundTrip(t *testing.T) {
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skip("macOS only")
	}
	text := "привет 👋\nline 2 <&> \"quotes\""
	home, _ := fakeText(t, text)
	payload, summary, err := home.Export()
	require.NoError(t, err)
	assert.Contains(t, summary, "chars")

	lint := exec.Command("/usr/bin/plutil", "-lint", "-")
	lint.Stdin = bytes.NewReader(payload)
	out, err := lint.CombinedOutput()
	require.NoError(t, err, string(out))

	// The helper on the other Mac sends binary plists: both forms must work.
	for name, p := range map[string][]byte{"xml": payload, "binary": toBinary(t, payload)} {
		work, copied := fakeText(t, "")
		_, err := work.Import(p)
		require.NoError(t, err, name)
		assert.Equal(t, text, string(*copied), name)
	}
}

func TestTextClipboard_RichPayloadFromHelper(t *testing.T) {
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skip("macOS only")
	}
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>files</key><dict/>
<key>items</key><array>
  <dict><key>public.html</key><data>` + b64("<b>bold</b>") + `</data>
        <key>public.utf8-plain-text</key><data>` + b64("bold") + `</data></dict>
</array>
<key>version</key><integer>1</integer>
</dict></plist>`
	work, copied := fakeText(t, "")
	_, err := work.Import(toBinary(t, []byte(plist)))
	require.NoError(t, err)
	assert.Equal(t, "bold", string(*copied), "plain text is taken from a rich clipboard")

	img := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>items</key><array>
  <dict><key>public.png</key><data>` + b64("\x89PNGfake") + `</data></dict>
</array></dict></plist>`
	work, copied = fakeText(t, "")
	summary, err := work.Import([]byte(img))
	require.NoError(t, err)
	assert.Contains(t, summary, "image saved")
	data, err := os.ReadFile(string(*copied))
	require.NoError(t, err)
	assert.Equal(t, "\x89PNGfake", string(data))
	assert.Equal(t, work.SaveDir, filepath.Dir(string(*copied)))

	_, err = work.Import([]byte("garbage from a wrong key"))
	require.Error(t, err)

	empty, _ := fakeText(t, "")
	_, _, err = empty.Export()
	require.Error(t, err)

	assert.Error(t, empty.WatchDoubleCopy(context.Background(), time.Second, func() {}))
}
