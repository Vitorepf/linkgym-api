package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Vitorepf/linkgym-api/internal/auth"
	"github.com/Vitorepf/linkgym-api/internal/owner"
)

func (a *api) ownerReturns(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	items, err := a.owner.Returns(r.Context(), sess.Person.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) ownerAttention(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	items, err := a.owner.Attention(r.Context(), sess.Person.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) ownerApplyAttention(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	err := a.owner.Apply(r.Context(), sess.Person.ID, r.PathValue("id"))
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type applyReturnReq struct {
	BumpKg float64 `json:"bump_kg"`
}

func (a *api) ownerApplyReturn(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req applyReturnReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.owner.ApplyReturn(r.Context(), sess.Person.ID, r.PathValue("alert_id"), req.BumpKg)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type weekApproveReq struct {
	PersonIDs []string `json:"person_ids"`
}

func (a *api) ownerWeek(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	items, err := a.owner.Week(r.Context(), sess.Person.ID, r.URL.Query().Get("from"))
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) ownerWeekApprove(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req weekApproveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	n, err := a.owner.Approve(r.Context(), sess.Person.ID, req.PersonIDs)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

func (a *api) ownerStudent(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	got, err := a.owner.Student(r.Context(), sess.Person.ID, r.PathValue("id"))
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) ownerOperacao(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	got, err := a.owner.Operacao(r.Context(), sess.Person.ID)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) ownerPagarMensalidade(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	if err := a.owner.PagarMensalidade(r.Context(), sess.Person.ID, r.PathValue("bond_id")); err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Desfazer o "Recebi": o recibo da linha é o alvo do toque, sem diálogo de confirmação.
// Confirmação em toda ação é o que faz app parecer formulário — e aqui o desfazer É a
// confirmação, com janela infinita.
func (a *api) ownerDesfazerPagamento(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	if err := a.owner.DesfazerPagamento(r.Context(), sess.Person.ID, r.PathValue("bond_id")); err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// O QUE ELE VENDE FORA DA MENSALIDADE: dois campos, e a resposta já traz o Pix pronto —
// o gesto seguinte dele é mandar o código para a aluna, e um segundo pedido no meio disso
// seria uma espera no lugar de uma ação.
type extraReq struct {
	BondID     string `json:"bond_id"`
	Descricao  string `json:"descricao"`
	ValorCents int    `json:"valor_cents"`
}

func (a *api) ownerCriarExtra(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req extraReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	got, err := a.owner.CriarExtra(r.Context(), sess.Person.ID, req.BondID, req.Descricao, req.ValorCents)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *api) ownerReceberExtra(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	desfazer := r.Method == http.MethodDelete
	if err := a.owner.ReceberExtra(r.Context(), sess.Person.ID, r.PathValue("id"), desfazer); err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// O vínculo muda de estado: encerrar, pausar, reativar. Era o buraco mais caro do produto —
// `bonds.status` tinha os quatro estados no CHECK desde a primeira migration e nenhuma linha
// de Go escrevia dois deles.
type estadoDoVinculoReq struct {
	Estado string `json:"estado"`
}

func (a *api) ownerMudarEstadoDoVinculo(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req estadoDoVinculoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	err := a.owner.MudarEstadoDoVinculo(r.Context(), sess.Person.ID,
		r.PathValue("bond_id"), owner.EstadoDoVinculo(req.Estado))
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type mensalidadeReq struct {
	AmountCents int `json:"amount_cents"`
	DueDay      int `json:"due_day"`
}

func (a *api) ownerDefinirMensalidade(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req mensalidadeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	if err := a.owner.DefinirMensalidade(r.Context(), sess.Person.ID, r.PathValue("bond_id"), req.AmountCents, req.DueDay); err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type timePatchReq struct {
	Name        *string           `json:"name"`
	AccentColor *string           `json:"accent_color"`
	LogoKey     *string           `json:"logo_object_key"`
	Config      *owner.TimeConfig `json:"config"`
	// A CHAVE PIX do personal e os dois campos que o padrão do Banco Central exige junto
	// dela. O dinheiro do aluno vai direto para a conta dele: a LinkGym monta o código e
	// não toca no dinheiro.
	ChavePix        *string `json:"chave_pix"`
	NomeRecebedor   *string `json:"nome_recebedor"`
	CidadeRecebedor *string `json:"cidade_recebedor"`
}

func (a *api) ownerPatchTime(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req timePatchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "json_invalido")
		return
	}
	// VALIDA TUDO ANTES DE GRAVAR QUALQUER COISA. Era o contrário: nome e cor gravavam,
	// a config validava depois, e uma frase com telefone devolvia 400 com a cor JÁ trocada.
	// O app dizia "não deu para salvar" e a marca do personal tinha mudado — provado no
	// aparelho: o acento virou #121111 num save reportado como falha.
	if req.Config != nil {
		if err := owner.ValidarConfig(req.Config); err != nil {
			writeOwnerError(w, err)
			return
		}
	}
	patch := owner.TimePatch{
		Name:            req.Name,
		Accent:          req.AccentColor,
		LogoKey:         req.LogoKey,
		ChavePix:        req.ChavePix,
		NomeRecebedor:   req.NomeRecebedor,
		CidadeRecebedor: req.CidadeRecebedor,
	}
	if !patch.Vazio() {
		if err := a.owner.PatchTime(r.Context(), sess.Person.ID, patch); err != nil {
			writeOwnerError(w, err)
			return
		}
	}
	if req.Config != nil {
		if err := a.owner.PatchTimeConfig(r.Context(), sess.Person.ID, req.Config); err != nil {
			writeOwnerError(w, err)
			return
		}
	}
	if patch.Vazio() && req.Config == nil {
		writeError(w, http.StatusBadRequest, "invalido")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type conviteReq struct {
	Phone string `json:"phone"`
}

// POST /v1/owner/invites — a PORTA. O convite é o primeiro artefato da marca do personal
// que sai do app: vai no WhatsApp com o nome do estúdio dele. A tabela existia desde a
// primeira migration e nenhuma rota criava um — só o seed de desenvolvimento.
func (a *api) ownerCriarConvite(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if sess.Person.Role != "owner" {
		writeError(w, http.StatusForbidden, "nao_autorizado")
		return
	}
	var req conviteReq
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	// Telefone é OPCIONAL: convite aberto serve para quem for. Quando vem, é normalizado
	// pelo mesmo caminho do login — senão o convite feito com "11 90000-0000" nunca casa
	// com o login feito com "+5511900000000".
	if req.Phone != "" {
		fone, err := auth.NormalizePhone(req.Phone)
		if err != nil {
			writeError(w, http.StatusBadRequest, "telefone_invalido")
			return
		}
		req.Phone = fone
	}
	convite, err := a.owner.CriarConvite(r.Context(), sess.Person.ID, req.Phone)
	if err != nil {
		writeOwnerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, convite)
}

func writeOwnerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, owner.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, owner.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, owner.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "erro")
	}
}
