// Package opensslenc reads and writes the format of
//
//	openssl enc -aes-256-ctr -pbkdf2 -salt -pass file:<keyfile>
//
// ("Salted__" + 8-byte salt + ciphertext; key and IV derived with
// PBKDF2-HMAC-SHA256, 10000 iterations), so data stays compatible with
// shell scripts that use openssl directly.
package opensslenc

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	magic      = "Salted__"
	saltLen    = 8
	iterations = 10000
	keyLen     = 32
	ivLen      = aes.BlockSize
)

// ReadPassword reads a password file the way `-pass file:` does: the first line.
func ReadPassword(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", fmt.Errorf("key file %s is empty", path)
	}
	return line, nil
}

func stream(password string, salt []byte) (cipher.Stream, error) {
	km, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyLen+ivLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(km[:keyLen])
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, km[keyLen:]), nil
}

// Encrypt returns plaintext encrypted with a fresh random salt.
func Encrypt(password string, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	s, err := stream(password, salt)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(magic)+saltLen+len(plaintext))
	copy(out, magic)
	copy(out[len(magic):], salt)
	s.XORKeyStream(out[len(magic)+saltLen:], plaintext)
	return out, nil
}

// ErrFormat means the data is not in openssl's salted format.
var ErrFormat = errors.New("not an openssl-encrypted blob")

// Decrypt reverses Encrypt. CTR mode has no integrity check: a wrong
// password yields garbage, which the caller must detect when parsing.
func Decrypt(password string, data []byte) ([]byte, error) {
	if len(data) < len(magic)+saltLen || !bytes.Equal(data[:len(magic)], []byte(magic)) {
		return nil, ErrFormat
	}
	s, err := stream(password, data[len(magic):len(magic)+saltLen])
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data)-len(magic)-saltLen)
	s.XORKeyStream(out, data[len(magic)+saltLen:])
	return out, nil
}
