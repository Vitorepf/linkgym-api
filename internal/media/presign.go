// Package media assina URLs S3/MinIO com SigV4 puro (crypto/hmac + sha256 da stdlib).
// Sem SDK de propósito: o app só precisa de DUAS operações — subir um objeto (PUT
// presignado) e mostrá-lo (GET presignado) — e um SDK inteiro para isso é dependência
// que ninguém audita. O MinIO do compose e qualquer S3 real falam o mesmo protocolo.
package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type Signer struct {
	endpoint  string // http://localhost:9000
	region    string
	bucket    string
	accessKey string
	secretKey string
	now       func() time.Time
}

// FromEnv lê S3_* do ambiente — os mesmos nomes do docker-compose e do .env.
func FromEnv(now func() time.Time) (*Signer, error) {
	if now == nil {
		now = time.Now
	}
	s := &Signer{
		endpoint:  strings.TrimSuffix(os.Getenv("S3_ENDPOINT"), "/"),
		region:    os.Getenv("S3_REGION"),
		bucket:    os.Getenv("S3_BUCKET"),
		accessKey: os.Getenv("S3_ACCESS_KEY"),
		secretKey: os.Getenv("S3_SECRET_KEY"),
		now:       now,
	}
	if s.region == "" {
		s.region = "us-east-1"
	}
	if s.endpoint == "" || s.bucket == "" || s.accessKey == "" || s.secretKey == "" {
		return nil, fmt.Errorf("media: S3_ENDPOINT/S3_BUCKET/S3_ACCESS_KEY/S3_SECRET_KEY ausentes")
	}
	return s, nil
}

// PresignPut devolve a URL onde o app faz PUT do arquivo, válida por 10 minutos.
func (s *Signer) PresignPut(objectKey string) string {
	return s.presign("PUT", objectKey, 10*time.Minute)
}

// PresignGet devolve a URL de leitura, válida por 6 dias (o máximo do SigV4 é 7).
func (s *Signer) PresignGet(objectKey string) string {
	return s.presign("GET", objectKey, 6*24*time.Hour)
}

func (s *Signer) presign(method, objectKey string, expires time.Duration) string {
	u, _ := url.Parse(s.endpoint)
	host := u.Host

	t := s.now().UTC()
	amzDate := t.Format("20060102T150405Z")
	shortDate := t.Format("20060102")
	scope := shortDate + "/" + s.region + "/s3/aws4_request"

	// path-style: /bucket/key — é o que o MinIO serve sem DNS de bucket.
	path := "/" + s.bucket + "/" + escapePath(objectKey)

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", s.accessKey+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(expires.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")

	canonicalQuery := canonicalQueryString(q)
	canonicalRequest := strings.Join([]string{
		method,
		path,
		canonicalQuery,
		"host:" + host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	toSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hexSHA256(canonicalRequest),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+s.secretKey), shortDate)
	key = hmacSHA256(key, s.region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))

	return s.endpoint + path + "?" + canonicalQuery + "&X-Amz-Signature=" + signature
}

// escapePath codifica cada segmento como o SigV4 exige (espaço vira %20, barra fica).
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = strings.ReplaceAll(url.QueryEscape(seg), "+", "%20")
	}
	return strings.Join(parts, "/")
}

func canonicalQueryString(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, escapeQuery(k)+"="+escapeQuery(q.Get(k)))
	}
	return strings.Join(pairs, "&")
}

func escapeQuery(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func hexSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}
