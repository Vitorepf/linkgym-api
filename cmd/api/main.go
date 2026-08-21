package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/auth"
	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/media"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/onboard"
	"github.com/Vitorepf/linkgym-api/internal/owner"
	"github.com/Vitorepf/linkgym-api/internal/progress"
	"github.com/Vitorepf/linkgym-api/internal/publish"
	"github.com/Vitorepf/linkgym-api/internal/relogio"
	"github.com/Vitorepf/linkgym-api/internal/today"
	"github.com/Vitorepf/linkgym-api/internal/workout"
)

func main() {
	config.LoadDotEnv(".env")

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	database, err := db.Open(url)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := migrate.Up(database); err != nil {
		log.Fatal(err)
	}

	addr := ":" + config.Getenv("PORT", "8080")
	dev := config.Getenv("ENV", "development") == "development"
	// Sem storage o app continua de pé: avatar/logo caem nas iniciais e o presign
	// responde 503 — imagem é acabamento, não fundação.
	signer, err := media.FromEnv(nil)
	if err != nil {
		log.Printf("media desligada: %v", err)
		signer = nil
	}
	api := &api{
		db:   database,
		auth: auth.New(database, config.Getenv("AUTH_PEPPER", ""), dev),
		// relogio.Agora, e nao time.Now: TODO servico monta a data com
		// now.Format("2006-01-02"), e a maquina e um conteiner sem tzdata, ou seja UTC.
		// Trocar o relogio AQUI conserta a ficha de hoje, a competencia da mensalidade e
		// os dias parados de uma vez, sem tocar em nenhum servico. Ver internal/relogio.
		today:    today.New(database, relogio.Agora),
		owner:    owner.New(database, relogio.Agora),
		progress: progress.New(database, relogio.Agora),
		publish:  publish.New(database, relogio.Agora),
		workout:  workout.New(database, relogio.Agora),
		onboard:  onboard.New(database, relogio.Agora),
		media:    signer,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("POST /v1/auth/code", api.requestCode)
	mux.HandleFunc("POST /v1/auth/verify", api.verify)
	mux.HandleFunc("GET /v1/me", api.me)
	mux.HandleFunc("PATCH /v1/me", api.withPerson(api.mePatch))
	mux.HandleFunc("POST /v1/media/presign", api.withPerson(api.mediaPresign))
	mux.HandleFunc("GET /v1/media/{key...}", api.mediaGet)
	mux.HandleFunc("PUT /v1/media/{key...}", api.withPerson(api.mediaPut))
	mux.HandleFunc("POST /v1/auth/logout", api.logout)
	mux.HandleFunc("GET /v1/today", api.withPerson(api.todayGet))
	mux.HandleFunc("PUT /v1/today/prontidao", api.withPerson(api.todayProntidaoPut))
	mux.HandleFunc("GET /v1/owner/home", api.withPerson(api.ownerHome))
	mux.HandleFunc("GET /v1/owner/attention", api.withPerson(api.ownerAttention))
	mux.HandleFunc("POST /v1/owner/attention/{id}/apply", api.withPerson(api.ownerApplyAttention))
	mux.HandleFunc("GET /v1/owner/week", api.withPerson(api.ownerWeek))
	mux.HandleFunc("POST /v1/owner/week/approve", api.withPerson(api.ownerWeekApprove))
	mux.HandleFunc("GET /v1/owner/students/{id}", api.withPerson(api.ownerStudent))
	mux.HandleFunc("GET /v1/owner/operacao", api.withPerson(api.ownerOperacao))
	mux.HandleFunc("POST /v1/owner/mensalidades/{bond_id}/pagar", api.withPerson(api.ownerPagarMensalidade))
	mux.HandleFunc("DELETE /v1/owner/mensalidades/{bond_id}/pagar", api.withPerson(api.ownerDesfazerPagamento))
	mux.HandleFunc("PUT /v1/owner/mensalidades/{bond_id}", api.withPerson(api.ownerDefinirMensalidade))
	mux.HandleFunc("PUT /v1/owner/bonds/{bond_id}/estado", api.withPerson(api.ownerMudarEstadoDoVinculo))
	mux.HandleFunc("GET /v1/student/mensalidade", api.withPerson(api.studentMensalidade))
	mux.HandleFunc("POST /v1/student/ja-paguei", api.withPerson(api.studentJaPaguei))
	mux.HandleFunc("GET /v1/student/loja", api.withPerson(api.studentLoja))
	mux.HandleFunc("POST /v1/student/loja/{id}/quero", api.withPerson(api.studentQuero))
	mux.HandleFunc("DELETE /v1/student/loja/{id}/quero", api.withPerson(api.studentDesistir))
	mux.HandleFunc("GET /v1/owner/produtos/modelos", api.withPerson(api.ownerModelosDeProduto))
	mux.HandleFunc("POST /v1/owner/produtos", api.withPerson(api.ownerCriarProduto))
	mux.HandleFunc("PUT /v1/owner/produtos/{id}/ativo", api.withPerson(api.ownerPausarProduto))
	mux.HandleFunc("POST /v1/owner/produtos/{id}/vender", api.withPerson(api.ownerVenderProduto))
	mux.HandleFunc("POST /v1/owner/produtos/{id}/assinar", api.withPerson(api.ownerAssinar))
	mux.HandleFunc("POST /v1/owner/assinaturas/{id}/recebi", api.withPerson(api.ownerReceberAssinatura))
	mux.HandleFunc("DELETE /v1/owner/assinaturas/{id}", api.withPerson(api.ownerCancelarAssinatura))
	mux.HandleFunc("POST /v1/owner/extras/{id}/uso", api.withPerson(api.ownerGastarSessao))
	mux.HandleFunc("POST /v1/owner/toques", api.withPerson(api.ownerRegistrarToque))
	mux.HandleFunc("POST /v1/owner/extras", api.withPerson(api.ownerCriarExtra))
	mux.HandleFunc("POST /v1/owner/extras/{id}/recebi", api.withPerson(api.ownerReceberExtra))
	mux.HandleFunc("DELETE /v1/owner/extras/{id}/recebi", api.withPerson(api.ownerReceberExtra))
	mux.HandleFunc("PATCH /v1/owner/time", api.withPerson(api.ownerPatchTime))
	mux.HandleFunc("POST /v1/owner/invites", api.withPerson(api.ownerCriarConvite))
	mux.HandleFunc("GET /v1/owner/returns", api.withPerson(api.ownerReturns))
	mux.HandleFunc("POST /v1/owner/returns/{alert_id}/apply", api.withPerson(api.ownerApplyReturn))
	mux.HandleFunc("POST /v1/comebacks/{id}/complete", api.withPerson(api.comebackComplete))
	mux.HandleFunc("POST /v1/sessions", api.withPerson(api.sessionStart))
	mux.HandleFunc("POST /v1/sessions/{id}/sets", api.withPerson(api.sessionAddSet))
	mux.HandleFunc("POST /v1/sessions/{id}/swap", api.withPerson(api.sessionSwap))
	mux.HandleFunc("POST /v1/sessions/{id}/finish", api.withPerson(api.sessionFinish))
	mux.HandleFunc("GET /v1/progress", api.withPerson(api.progressGet))
	mux.HandleFunc("GET /v1/records", api.withPerson(api.recordsGet))
	mux.HandleFunc("GET /v1/exercises", api.withPerson(api.exercisesList))
	mux.HandleFunc("POST /v1/exercises", api.withPerson(api.exerciseCreate))
	mux.HandleFunc("PATCH /v1/exercises/{id}", api.withPerson(api.exerciseRename))
	mux.HandleFunc("GET /v1/models", api.withPerson(api.modelsList))
	mux.HandleFunc("POST /v1/models", api.withPerson(api.modelCreate))
	mux.HandleFunc("GET /v1/models/{id}", api.withPerson(api.modelGet))
	mux.HandleFunc("PATCH /v1/models/{id}", api.withPerson(api.modelRename))
	mux.HandleFunc("DELETE /v1/models/{id}", api.withPerson(api.modelDelete))
	mux.HandleFunc("PUT /v1/models/{id}/items", api.withPerson(api.modelItemsPut))
	mux.HandleFunc("POST /v1/models/{id}/draft-from-last", api.withPerson(api.draftFromLast))
	mux.HandleFunc("PATCH /v1/prescriptions/{id}/items/{item_id}", api.withPerson(api.patchPrescriptionItem))
	mux.HandleFunc("POST /v1/prescriptions/{id}/items/{item_id}/swap", api.withPerson(api.swapPrescriptionItem))
	mux.HandleFunc("POST /v1/publish", api.withPerson(api.publishPost))
	mux.HandleFunc("PUT /v1/onboarding", api.withPerson(api.onboardingPut))
	mux.HandleFunc("PUT /v1/commitment", api.withPerson(api.commitmentPut))

	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("linkgym-api listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

type api struct {
	db       *sql.DB
	auth     *auth.Service
	today    *today.Service
	owner    *owner.Service
	progress *progress.Service
	publish  *publish.Service
	workout  *workout.Service
	onboard  *onboard.Service
	media    *media.Signer
}

func (a *api) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := "ok"
	if a.db != nil {
		if err := a.db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "down",
				"service": "linkgym-api",
				"db":      "down",
			})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  status,
		"service": "linkgym-api",
		"db":      "ok",
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
