package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestModelsForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.modelsList)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"nao_autorizado"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestDraftFromLastForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/models/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/draft-from-last", bytes.NewBufferString(`{"person_id":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rec := httptest.NewRecorder()
	a.withPerson(a.draftFromLast)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestPublishForbiddenAsStudentHTTP(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneVitor)
	req := httptest.NewRequest(http.MethodPost, "/v1/publish", bytes.NewBufferString(`{"prescription_id":"x","also_person_ids":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.publishPost)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestPatchPublishedItemHTTPInvalid(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneFred)
	vitorID := personIDByPhone(t, a.db, seed.PhoneVitor)

	var prID, itemID string
	if err := a.db.QueryRow(`
		SELECT pr.id::text, pi.id::text
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prID, &itemID); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"load_kg": 42.5, "planned_sets": 3, "planned_reps": "8-12",
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/prescriptions/"+prID+"/items/"+itemID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", prID)
	req.SetPathValue("item_id", itemID)
	rec := httptest.NewRecorder()
	a.withPerson(a.patchPrescriptionItem)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestModelsListHTTP(t *testing.T) {
	a := testAPI(t)
	token := loginToken(t, a, seed.PhoneFred)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.withPerson(a.modelsList)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"name":"Treino A"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func personIDByPhone(t *testing.T, database *sql.DB, phone string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT id FROM people WHERE phone = $1`, phone).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// ---------------------------------------------------------------- biblioteca e Modelo

func nomeUnico(prefixo string) string {
	return fmt.Sprintf("%s %d", prefixo, time.Now().UnixNano())
}

func chamar(t *testing.T, a *api, h http.HandlerFunc, phone, method, target, body string, path map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	token := loginToken(t, a, phone)
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range path {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	a.withPerson(h)(rec, req)
	return rec
}

type corpoModelo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Items []struct {
		ExerciseID string `json:"exercise_id"`
		Name       string `json:"name"`
		Position   int    `json:"position"`
	} `json:"items"`
}

func decodeModelo(t *testing.T, rec *httptest.ResponseRecorder) corpoModelo {
	t.Helper()
	var out corpoModelo
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json %v body %s", err, rec.Body.String())
	}
	return out
}

func exerciseIDByName(t *testing.T, database *sql.DB, name string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`
		SELECT e.id::text FROM exercises e
		JOIN studios s ON s.id = e.studio_id
		JOIN people o ON o.id = s.owner_person_id
		WHERE o.phone = $1 AND e.name = $2`,
		seed.PhoneFred, name,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// exercicioDeOutroTime monta um Time separado so para provar que a biblioteca nao
// atravessa: o mesmo nome existe nos dois lados e o id de la nao pode entrar em nada daqui.
func exercicioDeOutroTime(t *testing.T, database *sql.DB) string {
	t.Helper()
	var personID, studioID, exID string
	if err := database.QueryRow(`
		INSERT INTO people (phone, name) VALUES ('+5511900000099', 'Outra')
		ON CONFLICT (phone) DO UPDATE SET updated_at = now()
		RETURNING id::text`).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO studios (owner_person_id, name) VALUES ($1, 'Outra')
		ON CONFLICT (owner_person_id) DO UPDATE SET updated_at = now()
		RETURNING id::text`, personID).Scan(&studioID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO exercises (studio_id, name) VALUES ($1, 'Supino')
		ON CONFLICT (studio_id, name) DO UPDATE SET updated_at = now()
		RETURNING id::text`, studioID).Scan(&exID); err != nil {
		t.Fatal(err)
	}
	return exID
}

func modeloNovo(t *testing.T, a *api) corpoModelo {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name": nomeUnico("Estrutura"),
		"items": []map[string]any{
			{"exercise_id": exerciseIDByName(t, a.db, "Supino"), "planned_sets": 3, "planned_reps": "8-12"},
		},
	})
	rec := chamar(t, a, a.modelCreate, seed.PhoneFred, http.MethodPost, "/v1/models", string(body), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	m := decodeModelo(t, rec)
	t.Cleanup(func() {
		_, _ = a.db.Exec(`DELETE FROM models WHERE id = $1`, m.ID)
	})
	return m
}

func TestExerciseCreateHTTP(t *testing.T) {
	a := testAPI(t)
	nome := nomeUnico("Serrote")
	t.Cleanup(func() { _, _ = a.db.Exec(`DELETE FROM exercises WHERE name = $1`, nome) })

	rec := chamar(t, a, a.exerciseCreate, seed.PhoneFred, http.MethodPost, "/v1/exercises",
		`{"name":"`+nome+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"name":"`+nome+`"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}

	lista := chamar(t, a, a.exercisesList, seed.PhoneFred, http.MethodGet, "/v1/exercises", "", nil)
	if !bytes.Contains(lista.Body.Bytes(), []byte(nome)) {
		t.Fatalf("biblioteca sem o exercicio: %s", lista.Body.String())
	}
}

func TestExerciseCreateEmptyNameHTTP(t *testing.T) {
	a := testAPI(t)
	rec := chamar(t, a, a.exerciseCreate, seed.PhoneFred, http.MethodPost, "/v1/exercises", `{"name":"   "}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestExerciseCreateDuplicateHTTP(t *testing.T) {
	a := testAPI(t)
	rec := chamar(t, a, a.exerciseCreate, seed.PhoneFred, http.MethodPost, "/v1/exercises", `{"name":"Supino"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestExerciseCreateForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	rec := chamar(t, a, a.exerciseCreate, seed.PhoneVitor, http.MethodPost, "/v1/exercises", `{"name":"Serrote"}`, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"nao_autorizado"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestExerciseRenameHTTP(t *testing.T) {
	a := testAPI(t)
	antes, depois := nomeUnico("Serrote"), nomeUnico("Serrote curto")
	t.Cleanup(func() { _, _ = a.db.Exec(`DELETE FROM exercises WHERE name = $1 OR name = $2`, antes, depois) })

	criado := chamar(t, a, a.exerciseCreate, seed.PhoneFred, http.MethodPost, "/v1/exercises", `{"name":"`+antes+`"}`, nil)
	if criado.Code != http.StatusOK {
		t.Fatalf("status %d body %s", criado.Code, criado.Body.String())
	}
	var novo struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(criado.Body.Bytes(), &novo); err != nil {
		t.Fatal(err)
	}

	rec := chamar(t, a, a.exerciseRename, seed.PhoneFred, http.MethodPatch, "/v1/exercises/"+novo.ID,
		`{"name":"`+depois+`"}`, map[string]string{"id": novo.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var nome string
	if err := a.db.QueryRow(`SELECT name FROM exercises WHERE id = $1`, novo.ID).Scan(&nome); err != nil {
		t.Fatal(err)
	}
	if nome != depois {
		t.Fatalf("nome %q", nome)
	}
}

func TestExerciseRenameDuplicateHTTP(t *testing.T) {
	a := testAPI(t)
	id := exerciseIDByName(t, a.db, "Remada")
	rec := chamar(t, a, a.exerciseRename, seed.PhoneFred, http.MethodPatch, "/v1/exercises/"+id,
		`{"name":"Supino"}`, map[string]string{"id": id})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestExerciseRenameOtherTimeHTTP(t *testing.T) {
	a := testAPI(t)
	id := exercicioDeOutroTime(t, a.db)
	rec := chamar(t, a, a.exerciseRename, seed.PhoneFred, http.MethodPatch, "/v1/exercises/"+id,
		`{"name":"`+nomeUnico("Serrote")+`"}`, map[string]string{"id": id})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestExerciseRenameForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	id := exerciseIDByName(t, a.db, "Supino")
	rec := chamar(t, a, a.exerciseRename, seed.PhoneVitor, http.MethodPatch, "/v1/exercises/"+id,
		`{"name":"Serrote"}`, map[string]string{"id": id})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestModelItemsPutHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	supino := exerciseIDByName(t, a.db, "Supino")
	remada := exerciseIDByName(t, a.db, "Remada")

	body, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"exercise_id": remada, "planned_sets": 4, "planned_reps": "10"},
		{"exercise_id": supino, "planned_sets": 3, "planned_reps": "8-12"},
	}})
	rec := chamar(t, a, a.modelItemsPut, seed.PhoneFred, http.MethodPut, "/v1/models/"+m.ID+"/items",
		string(body), map[string]string{"id": m.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	got := decodeModelo(t, rec)
	if len(got.Items) != 2 {
		t.Fatalf("itens %d body %s", len(got.Items), rec.Body.String())
	}
	if got.Items[0].ExerciseID != remada || got.Items[0].Position != 1 {
		t.Fatalf("posicao 1 %+v", got.Items[0])
	}
	if got.Items[1].ExerciseID != supino || got.Items[1].Position != 2 {
		t.Fatalf("posicao 2 %+v", got.Items[1])
	}

	// A lista inteira e substituida: trocar a ordem tem que passar apesar do
	// model_items_position UNIQUE (model_id, position).
	invertido, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"exercise_id": supino, "planned_sets": 3, "planned_reps": "8-12"},
		{"exercise_id": remada, "planned_sets": 4, "planned_reps": "10"},
	}})
	rec2 := chamar(t, a, a.modelItemsPut, seed.PhoneFred, http.MethodPut, "/v1/models/"+m.ID+"/items",
		string(invertido), map[string]string{"id": m.ID})
	if rec2.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec2.Code, rec2.Body.String())
	}
	got2 := decodeModelo(t, rec2)
	if len(got2.Items) != 2 || got2.Items[0].ExerciseID != supino || got2.Items[1].ExerciseID != remada {
		t.Fatalf("ordem nao trocou: %s", rec2.Body.String())
	}
}

func TestModelItemsPutEmptyHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	rec := chamar(t, a, a.modelItemsPut, seed.PhoneFred, http.MethodPut, "/v1/models/"+m.ID+"/items",
		`{"items":[]}`, map[string]string{"id": m.ID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestModelItemsPutDuplicateExerciseHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	supino := exerciseIDByName(t, a.db, "Supino")
	body, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"exercise_id": supino, "planned_sets": 3, "planned_reps": "8-12"},
		{"exercise_id": supino, "planned_sets": 3, "planned_reps": "8-12"},
	}})
	rec := chamar(t, a, a.modelItemsPut, seed.PhoneFred, http.MethodPut, "/v1/models/"+m.ID+"/items",
		string(body), map[string]string{"id": m.ID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestModelItemsPutOtherTimeExerciseHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	body, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"exercise_id": exercicioDeOutroTime(t, a.db), "planned_sets": 3, "planned_reps": "8-12"},
	}})
	rec := chamar(t, a, a.modelItemsPut, seed.PhoneFred, http.MethodPut, "/v1/models/"+m.ID+"/items",
		string(body), map[string]string{"id": m.ID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	// A lista antiga continua de pe: recusa nao pode deixar o Modelo vazio.
	got := decodeModelo(t, chamar(t, a, a.modelGet, seed.PhoneFred, http.MethodGet, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID}))
	if len(got.Items) != 1 {
		t.Fatalf("itens %d", len(got.Items))
	}
}

func TestModelItemsPutForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	rec := chamar(t, a, a.modelItemsPut, seed.PhoneVitor, http.MethodPut,
		"/v1/models/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/items", `{"items":[]}`,
		map[string]string{"id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestModelRenameHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	novo := nomeUnico("Estrutura")
	rec := chamar(t, a, a.modelRename, seed.PhoneFred, http.MethodPatch, "/v1/models/"+m.ID,
		`{"name":"`+novo+`"}`, map[string]string{"id": m.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	got := decodeModelo(t, chamar(t, a, a.modelGet, seed.PhoneFred, http.MethodGet, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID}))
	if got.Name != novo {
		t.Fatalf("nome %q", got.Name)
	}
}

func TestModelRenameDuplicateHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	rec := chamar(t, a, a.modelRename, seed.PhoneFred, http.MethodPatch, "/v1/models/"+m.ID,
		`{"name":"Treino A"}`, map[string]string{"id": m.ID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"invalido"`)) {
		t.Fatalf("body %s", rec.Body.String())
	}
}

// A Prescricao aponta para o Modelo com ON DELETE RESTRICT: cada uma e o Modelo aplicado a
// UMA pessoa, com as cargas dela. Apagar a linha levaria a ficha de quem ja treinou junto,
// entao o DELETE arquiva — some da biblioteca, e a Prescricao continua inteira.
func TestModelDeleteKeepsPrescriptionHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	vitorID := personIDByPhone(t, a.db, seed.PhoneVitor)

	var prID string
	if err := a.db.QueryRow(`
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status)
		SELECT studio_id, $1, id, current_date + 30, 'draft' FROM models WHERE id = $2
		RETURNING id::text`, vitorID, m.ID).Scan(&prID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = a.db.Exec(`DELETE FROM prescriptions WHERE id = $1`, prID) })

	rec := chamar(t, a, a.modelDelete, seed.PhoneFred, http.MethodDelete, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var vivas int
	if err := a.db.QueryRow(`SELECT count(*) FROM prescriptions WHERE id = $1`, prID).Scan(&vivas); err != nil {
		t.Fatal(err)
	}
	if vivas != 1 {
		t.Fatalf("prescricao %d", vivas)
	}
	var arquivado bool
	if err := a.db.QueryRow(`SELECT archived_at IS NOT NULL FROM models WHERE id = $1`, m.ID).Scan(&arquivado); err != nil {
		t.Fatal(err)
	}
	if !arquivado {
		t.Fatal("modelo continua na biblioteca")
	}

	sumiu := chamar(t, a, a.modelGet, seed.PhoneFred, http.MethodGet, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID})
	if sumiu.Code != http.StatusNotFound {
		t.Fatalf("get status %d", sumiu.Code)
	}
	lista := chamar(t, a, a.modelsList, seed.PhoneFred, http.MethodGet, "/v1/models", "", nil)
	if bytes.Contains(lista.Body.Bytes(), []byte(m.Name)) {
		t.Fatalf("ainda na lista: %s", lista.Body.String())
	}
}

// Arquivar libera o nome: o personal que errou o nome apaga e escreve de novo.
func TestModelDeleteFreesNameHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	if rec := chamar(t, a, a.modelDelete, seed.PhoneFred, http.MethodDelete, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID}); rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	body, _ := json.Marshal(map[string]any{
		"name": m.Name,
		"items": []map[string]any{
			{"exercise_id": exerciseIDByName(t, a.db, "Supino"), "planned_sets": 3, "planned_reps": "8-12"},
		},
	})
	rec := chamar(t, a, a.modelCreate, seed.PhoneFred, http.MethodPost, "/v1/models", string(body), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	t.Cleanup(func() { _, _ = a.db.Exec(`DELETE FROM models WHERE id = $1`, decodeModelo(t, rec).ID) })
}

func TestModelDeleteTwiceHTTP(t *testing.T) {
	a := testAPI(t)
	m := modeloNovo(t, a)
	if rec := chamar(t, a, a.modelDelete, seed.PhoneFred, http.MethodDelete, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID}); rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	rec := chamar(t, a, a.modelDelete, seed.PhoneFred, http.MethodDelete, "/v1/models/"+m.ID, "", map[string]string{"id": m.ID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestModelDeleteForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	rec := chamar(t, a, a.modelDelete, seed.PhoneVitor, http.MethodDelete,
		"/v1/models/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "",
		map[string]string{"id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestModelRenameForbiddenAsStudent(t *testing.T) {
	a := testAPI(t)
	rec := chamar(t, a, a.modelRename, seed.PhoneVitor, http.MethodPatch,
		"/v1/models/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", `{"name":"x"}`,
		map[string]string{"id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
