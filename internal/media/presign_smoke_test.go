package media

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/config"
)

// Prova executável contra o MinIO real do compose: assina, sobe, lê de volta.
// Sem MinIO no ar o teste pula — CI sem storage não vira vermelho mentiroso.
func TestPresignRoundTrip(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
			config.LoadDotEnv(filepath.Join(dir, ".env"))
			break
		}
		dir = filepath.Dir(dir)
	}
	s, err := FromEnv(nil)
	if err != nil {
		t.Skip("S3_* ausentes:", err)
	}
	putURL := s.PresignPut("smoke/teste.txt")
	req, _ := http.NewRequest("PUT", putURL, bytes.NewReader([]byte("linkgym")))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skip("MinIO fora do ar:", err)
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("PUT %d: %s", res.StatusCode, b)
	}
	res2, err := http.Get(s.PresignGet("smoke/teste.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res2.Body)
	if res2.StatusCode != 200 || string(body) != "linkgym" {
		t.Fatalf("GET %d: %q", res2.StatusCode, body)
	}
}
