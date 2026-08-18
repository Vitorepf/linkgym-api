package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
)

const DevCode = "0000"

func hashBytes(pepper, phone, secret string) []byte {
	sum := sha256.Sum256([]byte(pepper + "\n" + phone + "\n" + secret))
	return sum[:]
}

func randomDigits(n int) (string, error) {
	out := make([]byte, n)
	for i := range out {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		out[i] = '0' + byte(d.Int64())
	}
	return string(out), nil
}

func randomToken() (raw string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, buf); err != nil {
		return "", nil, err
	}
	raw = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

func tokenHash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func fmtErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
