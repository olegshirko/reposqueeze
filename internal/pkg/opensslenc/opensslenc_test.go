package opensslenc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	data := []byte("hello\x00binary\nclipboard")
	enc, err := Encrypt("secret", data)
	require.NoError(t, err)
	assert.Equal(t, "Salted__", string(enc[:8]))

	dec, err := Decrypt("secret", enc)
	require.NoError(t, err)
	assert.Equal(t, data, dec)

	wrong, err := Decrypt("other", enc)
	require.NoError(t, err)
	assert.NotEqual(t, data, wrong)

	_, err = Decrypt("secret", []byte("plain"))
	assert.ErrorIs(t, err, ErrFormat)
}

// The format must match `openssl enc -aes-256-ctr -pbkdf2`, which the
// existing clipsync shell scripts use, in both directions.
func TestCompatibleWithOpenSSL(t *testing.T) {
	bin, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not installed")
	}
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	require.NoError(t, os.WriteFile(keyFile, []byte("c2VjcmV0LWtleS1mb3ItdGVzdHM=\nignored second line\n"), 0o600))
	pass, err := ReadPassword(keyFile)
	require.NoError(t, err)
	assert.Equal(t, "c2VjcmV0LWtleS1mb3ItdGVzdHM=", pass)

	data := bytes.Repeat([]byte("clipboard payload \x01\x02\xff "), 500)

	// Go encrypts, openssl decrypts.
	enc, err := Encrypt(pass, data)
	require.NoError(t, err)
	cmd := exec.Command(bin, "enc", "-d", "-aes-256-ctr", "-pbkdf2", "-pass", "file:"+keyFile)
	cmd.Stdin = bytes.NewReader(enc)
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, data, out)

	// openssl encrypts, Go decrypts.
	cmd = exec.Command(bin, "enc", "-aes-256-ctr", "-pbkdf2", "-salt", "-pass", "file:"+keyFile)
	cmd.Stdin = bytes.NewReader(data)
	enc, err = cmd.Output()
	require.NoError(t, err)
	dec, err := Decrypt(pass, enc)
	require.NoError(t, err)
	assert.Equal(t, data, dec)
}
