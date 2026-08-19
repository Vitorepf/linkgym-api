package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Vitorepf/linkgym-api/internal/auth"
)

// Tipos de imagem que o app manda (picker de foto/logo). Extensão sai daqui — nunca do
// nome do arquivo do cliente.
var imageExt = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

type presignReq struct {
	Kind        string `json:"kind"` // avatar | logo
	ContentType string `json:"content_type"`
}

// POST /v1/media/presign — a porta de subida de imagem: avatar da Pessoa (qualquer papel)
// ou logo do Time (só o dono). Devolve a chave e a URL assinada; o app faz o PUT direto
// no storage e depois grava a chave via PATCH /v1/me ou PATCH /v1/owner/time.
func (a *api) mediaPresign(w http.ResponseWriter, r *http.Request) {
	if a.media == nil {
		writeError(w, http.StatusServiceUnavailable, "sem_storage")
		return
	}
	sess := sessionFrom(r)
	var req presignReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	ext, ok := imageExt[req.ContentType]
	if !ok {
		writeError(w, http.StatusBadRequest, "tipo_invalido")
		return
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	nonce := hex.EncodeToString(buf)

	var key string
	switch req.Kind {
	case "avatar":
		key = "avatar/" + sess.Person.ID + "/" + nonce + "." + ext
	case "logo":
		if sess.Person.Role != "owner" {
			writeError(w, http.StatusForbidden, "nao_autorizado")
			return
		}
		key = "logo/" + sess.Time.ID + "/" + nonce + "." + ext
	default:
		writeError(w, http.StatusBadRequest, "kind_invalido")
		return
	}

	if _, err := a.db.ExecContext(r.Context(), `
		INSERT INTO media_objects (object_key, content_type, created_by_person_id)
		VALUES ($1, $2, $3)`,
		key, req.ContentType, sess.Person.ID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"object_key": key,
		// relativa, da própria API: o app prefixa com a base dele. Presignada do MinIO
		// ia presa ao host localhost e quebrava fora do simulador.
		"upload_url": "/v1/media/" + key,
	})
}

type mePatchReq struct {
	Name        *string `json:"name"`
	AvatarColor *string `json:"avatar_color"`
	AvatarKey   *string `json:"avatar_object_key"`
}

// PATCH /v1/me — a Pessoa edita o nome que aparece e o rosto (cor ou foto que subiu).
// Devolve o MESMO payload do GET /v1/me: o app troca o estado numa tacada.
func (a *api) mePatch(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req mePatchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	next, err := a.auth.PatchMe(r.Context(), sess.Person.ID, req.Name, req.AvatarColor, req.AvatarKey)
	if err != nil {
		switch {
		case err == auth.ErrInvalid:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "erro")
		}
		return
	}
	a.presignSession(next)
	writeJSON(w, http.StatusOK, map[string]any{
		"person":              next.Person,
		"time":                next.Time,
		"onboarding_complete": next.OnboardingComplete,
		"commitment_complete": next.CommitmentComplete,
		"debut":               next.Debut,
	})
}

// A URL de mídia é RELATIVA e da PRÓPRIA API (/v1/media/...): o app prefixa com a base
// que ele já usa para tudo. URL presignada do MinIO ia presa ao host da assinatura
// (localhost) — no celular físico localhost é o telefone, e trocar o host quebra a
// assinatura SigV4. Foi exatamente o defeito: logo subia e a prévia mostrava nada.
func (a *api) presignSession(s *auth.Session) {
	if a.media == nil || s == nil {
		return
	}
	if s.Person.AvatarKey != "" {
		s.Person.AvatarURL = "/v1/media/" + s.Person.AvatarKey
	}
	if s.Time.LogoKey != "" {
		s.Time.LogoURL = "/v1/media/" + s.Time.LogoKey
	}
}

// GET /v1/media/{key...} — a API busca no storage (assinando do lado do servidor, onde o
// host da assinatura é o certo) e serve. Sem autenticação DE PROPÓSITO: <Image> não manda
// Bearer, e a chave carrega um nonce aleatório — é URL-capacidade, do mesmo jeito que a
// presignada seria. Só os prefixos de imagem existem nesta porta.
func (a *api) mediaGet(w http.ResponseWriter, r *http.Request) {
	if a.media == nil {
		writeError(w, http.StatusServiceUnavailable, "sem_storage")
		return
	}
	key := r.PathValue("key")
	if !strings.HasPrefix(key, "avatar/") && !strings.HasPrefix(key, "logo/") {
		writeError(w, http.StatusNotFound, "nao_encontrado")
		return
	}
	res, err := http.Get(a.media.PresignGet(key))
	if err != nil {
		writeError(w, http.StatusBadGateway, "storage_fora")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeError(w, http.StatusNotFound, "nao_encontrado")
		return
	}
	if ct := res.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, res.Body)
}

// PUT /v1/media/{key...} — o upload também passa pela API, pelo mesmo motivo do GET.
// Autenticado, e a chave tem que ser do próprio dono: avatar/<person_id>/... ou
// logo/<studio_id>/... do time do owner.
func (a *api) mediaPut(w http.ResponseWriter, r *http.Request) {
	if a.media == nil {
		writeError(w, http.StatusServiceUnavailable, "sem_storage")
		return
	}
	sess := sessionFrom(r)
	key := r.PathValue("key")
	okAvatar := strings.HasPrefix(key, "avatar/"+sess.Person.ID+"/")
	okLogo := sess.Person.Role == "owner" && strings.HasPrefix(key, "logo/"+sess.Time.ID+"/")
	if !okAvatar && !okLogo {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	body := http.MaxBytesReader(w, r.Body, 8<<20) // 8MB: foto de galeria comprimida cabe
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPut, a.media.PresignPut(key), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro")
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "storage_fora")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "upload_falhou")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
