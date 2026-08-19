package publish

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/config"
	"github.com/Vitorepf/linkgym-api/internal/db"
	"github.com/Vitorepf/linkgym-api/internal/migrate"
	"github.com/Vitorepf/linkgym-api/internal/seed"
)

func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if ok {
		dir := filepath.Dir(file)
		for i := 0; i < 5; i++ {
			candidate := filepath.Join(dir, ".env")
			if _, err := os.Stat(candidate); err == nil {
				config.LoadDotEnv(candidate)
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	os.Exit(m.Run())
}

func openSeeded(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	database, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := migrate.Up(database); err != nil {
		t.Fatal(err)
	}
	if err := seed.Dev(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	return database
}

func personIDByPhone(t *testing.T, database *sql.DB, phone string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT id FROM people WHERE phone = $1`, phone).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func treinoA(t *testing.T, database *sql.DB) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`
		SELECT m.id::text FROM models m
		JOIN studios s ON s.id = m.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $1 AND m.name = 'Treino A'`,
		seed.PhoneFred,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func clockOnDBDay(t *testing.T, database *sql.DB) (time.Time, string) {
	t.Helper()
	var day time.Time
	if err := database.QueryRow(`SELECT current_date`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	now := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	return now, tomorrow
}

func clearTomorrow(t *testing.T, database *sql.DB, personIDs []string, tomorrow string) {
	t.Helper()
	for _, id := range personIDs {
		if _, err := database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id = $1 AND for_date = $2::date`,
			id, tomorrow,
		); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, id := range personIDs {
			_, _ = database.Exec(`
				DELETE FROM prescriptions
				WHERE person_id = $1 AND for_date = $2::date`,
				id, tomorrow,
			)
		}
	})
}

func firstLoad(t *testing.T, database *sql.DB, personID, forDate, status string) float64 {
	t.Helper()
	var load float64
	if err := database.QueryRow(`
		SELECT pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = $2::date AND pr.status = $3`,
		personID, forDate, status,
	).Scan(&load); err != nil {
		t.Fatal(err)
	}
	return load
}

func TestPublishLoteNeverCopiesLoad(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID, huanID}, tomorrow)

	draft, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	if draft.DraftID == "" || len(draft.Items) == 0 {
		t.Fatalf("draft %+v", draft)
	}
	if draft.Items[0].LoadKg != 40 {
		t.Fatalf("vitor draft %v want 40", draft.Items[0].LoadKg)
	}

	if err := svc.Publish(context.Background(), fredID, draft.DraftID, []string{huanID}, ""); err != nil {
		t.Fatal(err)
	}

	if v := firstLoad(t, database, vitorID, tomorrow, "published"); v != 40 {
		t.Fatalf("vitor %v want 40", v)
	}
	if h := firstLoad(t, database, huanID, tomorrow, "published"); h != 22.5 {
		t.Fatalf("huan %v want 22.5 (never 40)", h)
	}
}

func TestManualLoadSource(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	draft, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	itemID := draft.Items[0].ID
	if itemID == "" {
		t.Fatal("missing item id")
	}

	if err := svc.PatchItem(context.Background(), fredID, draft.DraftID, itemID, ItemPatch{
		LoadKg:      42.5,
		PlannedSets: draft.Items[0].PlannedSets,
		PlannedReps: draft.Items[0].PlannedReps,
	}); err != nil {
		t.Fatal(err)
	}

	var load float64
	var source string
	if err := database.QueryRow(`
		SELECT load_kg, load_source FROM prescription_items WHERE id = $1`,
		itemID,
	).Scan(&load, &source); err != nil {
		t.Fatal(err)
	}
	if load != 42.5 {
		t.Fatalf("load %v want 42.5", load)
	}
	if source != "manual" {
		t.Fatalf("source %s want manual", source)
	}
}

func TestListModelsIncludesTreinoA(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)

	got, err := svc.ListModels(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range got {
		if m.Name == "Treino A" && m.ID != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("models %+v", got)
	}
}

func TestGetModelItemsHaveStarterLoad(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	modelID := treinoA(t, database)

	got, err := svc.GetModel(context.Background(), fredID, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Treino A" || len(got.Items) != 3 {
		t.Fatalf("%+v", got)
	}
	if got.Items[0].Name != "Supino" || got.Items[0].StarterLoadKg != 20 || got.Items[0].PlannedSets != 3 {
		t.Fatalf("first %+v", got.Items[0])
	}
}

func TestDraftFromModelUsesStarter(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	draft, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "model")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Items[0].LoadKg != 20 || draft.Items[0].LoadSource != "starter" {
		t.Fatalf("starter %+v", draft.Items[0])
	}
}

func TestPatchPublishedIsInvalid(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)

	var prID, itemID string
	if err := database.QueryRow(`
		SELECT pr.id::text, pi.id::text
		FROM prescriptions pr
		JOIN prescription_items pi ON pi.prescription_id = pr.id AND pi.position = 1
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prID, &itemID); err != nil {
		t.Fatal(err)
	}

	err := svc.PatchItem(context.Background(), fredID, prID, itemID, ItemPatch{
		LoadKg: 42.5, PlannedSets: 3, PlannedReps: "8-12",
	})
	if err != ErrInvalid {
		t.Fatalf("got %v want %v", err, ErrInvalid)
	}
}

func TestPublishForbiddenAsStudent(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)

	_, err := svc.ListModels(context.Background(), vitorID)
	if err != ErrForbidden {
		t.Fatalf("list %v", err)
	}
	_, err = svc.DraftFromLast(context.Background(), vitorID, modelID, vitorID, "last")
	if err != ErrForbidden {
		t.Fatalf("draft %v", err)
	}
	err = svc.Publish(context.Background(), vitorID, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil, "")
	if err != ErrForbidden {
		t.Fatalf("publish %v", err)
	}
}

// Os DOIS caminhos de publicacao tem que resolver a MESMA carga para a MESMA pessoa.
//
// Existem dois: DraftFromLast (publicar para um) e publishFromModelo (publicar em lote).
// Cada um tinha a sua propria cascata, e elas estavam em ordem OPOSTA — uma punha a serie
// executada na frente, a outra punha a ficha anterior. Com isso, o mesmo aluno recebia
// cargas diferentes conforme o personal tivesse clicado "publicar para 1" ou "publicar
// para a turma", e ninguem consegue confiar num numero que muda conforme o caminho.
//
// Este teste NAO escolhe qual ordem vence: ele exige que as duas concordem. Continua
// valendo se a regra mudar, desde que mude nos dois lados.
func TestOsDoisCaminhosResolvemAMesmaCarga(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	modelID := treinoA(t, database)

	// O corpo do Vitor discorda da ficha dele: prescrito 40, executado 47,5. Sem essa
	// divergencia o teste passaria por coincidencia e nao provaria nada.
	var exID, studioID string
	if err := database.QueryRow(`
		SELECT mi.exercise_id::text, m.studio_id::text
		FROM model_items mi JOIN models m ON m.id = mi.model_id
		WHERE mi.model_id = $1 ORDER BY mi.position LIMIT 1`, modelID,
	).Scan(&exID, &studioID); err != nil {
		t.Fatal(err)
	}
	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at, finished_at)
		VALUES ($1, $2, gen_random_uuid(), now(), now())
		RETURNING id::text`, vitorID, studioID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, sessionID) })
	if _, err := database.Exec(`
		INSERT INTO workout_sets (session_id, exercise_id, client_set_id, set_index, reps, load_kg, performed_at)
		VALUES ($1, $2, gen_random_uuid(), 1, 8, 47.5,
		        (SELECT COALESCE(MAX(performed_at), now()) + interval '1 second' FROM workout_sets))`,
		sessionID, exID,
	); err != nil {
		t.Fatal(err)
	}

	// Caminho A: o Vitor e o dono do rascunho.
	clearTomorrow(t, database, []string{vitorID, huanID}, tomorrow)
	draftA, err := svc.DraftFromLast(context.Background(), fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	sozinho := draftA.Items[0].LoadKg

	// Caminho B: o Vitor entra de carona no lote de outra pessoa.
	clearTomorrow(t, database, []string{vitorID, huanID}, tomorrow)
	draftB, err := svc.DraftFromLast(context.Background(), fredID, modelID, huanID, "last")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Publish(context.Background(), fredID, draftB.DraftID, []string{vitorID}, ""); err != nil {
		t.Fatal(err)
	}
	noLote := firstLoad(t, database, vitorID, tomorrow, "published")

	if sozinho != noLote {
		t.Fatalf("a mesma pessoa recebeu cargas diferentes: sozinho=%v, no lote=%v. "+
			"Prescrito era 40 e o corpo executou 47,5 — as duas cascatas estao em ordem oposta.",
			sozinho, noLote)
	}
}

// A biblioteca do Time tem que ser LISTAVEL e tem que ser do Time certo.
//
// Sem esta rota nao existe montar estrutura de treino: o personal nao tinha como ver o que
// pode escolher. E a checagem de aluno importa tanto quanto a de conteudo — biblioteca e
// do Time, e quem nao e dono do Time nao a enxerga.
func TestListExercisesDaBibliotecaDoTime(t *testing.T) {
	database := openSeeded(t)
	now, _ := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	fredID := personIDByPhone(t, database, seed.PhoneFred)

	itens, err := svc.ListExercises(context.Background(), fredID)
	if err != nil {
		t.Fatal(err)
	}
	if len(itens) < 20 {
		t.Fatalf("biblioteca com %d exercicios; com menos de 20 nao da para montar estrutura nem variar", len(itens))
	}

	// Os tres nomes originais tem que sobreviver: o Modelo Treino A e varios testes de
	// outros pacotes os procuram por nome.
	querotres := map[string]bool{"Supino": false, "Remada": false, "Agachamento": false}
	nomes := map[string]int{}
	for _, e := range itens {
		if e.ID == "" {
			t.Fatalf("exercicio sem id: %+v", e)
		}
		nomes[e.Name]++
		if _, ok := querotres[e.Name]; ok {
			querotres[e.Name] = true
		}
	}
	for n, achou := range querotres {
		if !achou {
			t.Errorf("sumiu da biblioteca: %s", n)
		}
	}
	for n, c := range nomes {
		if c > 1 {
			t.Errorf("%s aparece %d vezes; a lista tem que ser unica por Time", n, c)
		}
	}

	// Aluno nao lista biblioteca de Time nenhum.
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	if _, err := svc.ListExercises(context.Background(), vitorID); err == nil {
		t.Fatal("aluno listou a biblioteca do Time")
	}
}

// O personal cria uma ESTRUTURA e ela vira Modelo do Time dele.
//
// Antes disto, INSERT INTO models so existia no seed: nao havia como criar treino nenhum
// pelo app. O teste cobra tambem o que o Modelo NAO pode ser — o verbete diz que ele e
// estrutura, e que publicar replica o modelo e nunca a carga.
func TestCreateModelMontaEstruturaENaoCarrega(t *testing.T) {
	database := openSeeded(t)
	now, _ := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)

	lib, err := svc.ListExercises(ctx, fredID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib) < 3 {
		t.Fatal("biblioteca pequena demais para o teste")
	}

	novo, err := svc.CreateModel(ctx, fredID, NewModel{
		Name: "Estrutura de teste",
		Items: []NewModelItem{
			{ExerciseID: lib[0].ID, PlannedSets: 4, PlannedReps: "8"},
			{ExerciseID: lib[1].ID, PlannedSets: 3, PlannedReps: "10-12"},
			{ExerciseID: lib[2].ID, PlannedSets: 3, PlannedReps: "12"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM models WHERE id = $1`, novo.ID) })

	if len(novo.Items) != 3 {
		t.Fatalf("itens = %d, queria 3", len(novo.Items))
	}
	// A ORDEM da lista vira a posicao: ninguem digita numero de exercicio a mao.
	for i, it := range novo.Items {
		if it.Position != i+1 {
			t.Fatalf("item %d com position %d", i, it.Position)
		}
	}
	if novo.Items[0].ExerciseID != lib[0].ID {
		t.Fatal("a ordem enviada nao foi respeitada")
	}
	// Sem starter, a estrutura nao carrega peso nenhum — quem tem historico nunca ve esse
	// numero, e quem nao tem recebe zero, que a tela mostra como ausencia.
	if novo.Items[0].StarterLoadKg != 0 {
		t.Fatalf("starter = %v, queria 0 quando nao foi enviado", novo.Items[0].StarterLoadKg)
	}

	// Estrutura vazia, exercicio repetido e exercicio de fora do Time sao todos recusados.
	if _, err := svc.CreateModel(ctx, fredID, NewModel{Name: "Vazia"}); err == nil {
		t.Error("criou estrutura sem exercicio nenhum")
	}
	if _, err := svc.CreateModel(ctx, fredID, NewModel{Name: "Repetida", Items: []NewModelItem{
		{ExerciseID: lib[0].ID, PlannedSets: 3, PlannedReps: "10"},
		{ExerciseID: lib[0].ID, PlannedSets: 3, PlannedReps: "10"},
	}}); err == nil {
		t.Error("criou estrutura com o mesmo exercicio duas vezes")
	}
	if _, err := svc.CreateModel(ctx, fredID, NewModel{Name: "De fora", Items: []NewModelItem{
		{ExerciseID: "00000000-0000-0000-0000-000000000000", PlannedSets: 3, PlannedReps: "10"},
	}}); err == nil {
		t.Error("criou estrutura com exercicio que nao e do Time")
	}

	// Aluno nao monta estrutura.
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	if _, err := svc.CreateModel(ctx, vitorID, NewModel{Name: "Do aluno", Items: []NewModelItem{
		{ExerciseID: lib[0].ID, PlannedSets: 3, PlannedReps: "10"},
	}}); err == nil {
		t.Error("aluno criou Modelo")
	}
}

// load_source nao pode LAVAR o chute do Modelo e devolve-lo como historico do aluno.
//
// O caminho do defeito: starter_load_kg e generico e igual para todo mundo; ele e copiado
// para a primeira ficha da pessoa; na publicacao seguinte a cascata le de
// prescription_items e carimbava 'history'. O personal via "veio do historico" e acreditava
// que era o numero daquele corpo — eram 18 kg iguais para os tres alunos.
//
// Huan nao tem nenhuma serie executada em Remada. Entao a fonte dele em Remada NAO pode
// ser 'history', por mais fichas que ja tenham passado por cima.
func TestFonteNaoLavaStarterComoHistorico(t *testing.T) {
	database := openSeeded(t)
	now, _ := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	huanID := personIDByPhone(t, database, seed.PhoneHuan)
	modelID := treinoA(t, database)

	var executadas int
	if err := database.QueryRow(`
		SELECT count(*) FROM workout_sets ws
		JOIN workout_sessions s ON s.id = ws.session_id
		JOIN exercises e ON e.id = ws.exercise_id
		WHERE s.person_id = $1 AND e.name = 'Remada'`, huanID,
	).Scan(&executadas); err != nil {
		t.Fatal(err)
	}
	if executadas != 0 {
		t.Skip("o seed mudou: Huan passou a ter Remada executada, e o teste perdeu o caso")
	}

	draft, err := svc.DraftFromLast(ctx, fredID, modelID, huanID, "last")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range draft.Items {
		if it.Name != "Remada" {
			continue
		}
		if it.LoadSource == "history" {
			t.Fatalf("Remada do Huan veio como history com %v kg, mas ele nunca executou Remada. "+
				"O chute do Modelo foi lavado e devolvido como historico do corpo.", it.LoadKg)
		}
		return
	}
	t.Fatal("Remada nao apareceu no rascunho")
}

// Trocar o exercicio NAO leva a carga junto.
//
// E o caso que estraga o aluno em silencio: o Vitor faz Supino com 40; o personal troca
// Supino por Leg press, que ele nunca fez. Se a carga atravessar a troca, ele chega na
// primeira serie com 40 kg num padrao motor que nunca executou. A carga tem que ser
// re-resolvida do zero pelo corpo dele NAQUELE exercicio, e sem historico ela e ausencia.
func TestTrocarExercicioNaoCarregaAChargaAntiga(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	draft, err := svc.DraftFromLast(ctx, fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	antigo := draft.Items[0]
	if antigo.LoadKg == 0 {
		t.Fatal("o teste precisa de um item com carga para provar que ela nao atravessa")
	}

	// Um exercicio que o Vitor nunca fez e que nao esta no Modelo.
	var novoID, novoNome string
	if err := database.QueryRow(`
		SELECT e.id::text, e.name FROM exercises e
		WHERE e.studio_id = (SELECT studio_id FROM prescriptions WHERE id = $1)
		  AND e.id NOT IN (SELECT exercise_id FROM prescription_items WHERE prescription_id = $1)
		  AND e.id NOT IN (
		      SELECT ws.exercise_id FROM workout_sets ws
		      JOIN workout_sessions s ON s.id = ws.session_id WHERE s.person_id = $2)
		ORDER BY e.name LIMIT 1`, draft.DraftID, vitorID,
	).Scan(&novoID, &novoNome); err != nil {
		t.Fatal(err)
	}

	if err := svc.SwapItem(ctx, fredID, draft.DraftID, antigo.ID, novoID); err != nil {
		t.Fatal(err)
	}

	var load float64
	var source, exID string
	if err := database.QueryRow(`
		SELECT load_kg, load_source, exercise_id::text FROM prescription_items WHERE id = $1`,
		antigo.ID,
	).Scan(&load, &source, &exID); err != nil {
		t.Fatal(err)
	}
	if exID != novoID {
		t.Fatal("o exercicio nao trocou")
	}
	if load != 0 {
		t.Fatalf("%s ficou com %v kg depois da troca; a carga de %v kg do exercicio antigo atravessou",
			novoNome, load, antigo.LoadKg)
	}
	if source == "history" {
		t.Fatalf("%s veio como history sem o Vitor nunca ter executado", novoNome)
	}

	// Trocar por um exercicio que JA esta na ficha e engano, e tem que ser recusado.
	outro := draft.Items[1].ExerciseID
	if err := svc.SwapItem(ctx, fredID, draft.DraftID, antigo.ID, outro); err == nil {
		t.Error("deixou repetir exercicio na mesma ficha")
	}
	// Aluno nao troca exercicio de ficha nenhuma.
	if err := svc.SwapItem(ctx, vitorID, draft.DraftID, antigo.ID, novoID); err == nil {
		t.Error("aluno trocou exercicio da propria ficha")
	}
}

// Serie de sessao ABERTA nao vira carga prescrita.
//
// Sem o filtro de finished_at, um AQUECIMENTO registrado numa sessao em andamento virava o
// numero da proxima ficha: o aluno com 40 kg na ficha e 10 kg de aquecimento agora recebia
// 10. internal/today/service.go ja filtrava e explicava o porque; as publicacoes nao.
func TestSessaoAbertaNaoViraCargaPrescrita(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	modelID := treinoA(t, database)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	antes, err := svc.DraftFromLast(ctx, fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	esperado := antes.Items[0].LoadKg
	exID := antes.Items[0].ExerciseID

	var studioID string
	if err := database.QueryRow(
		`SELECT studio_id::text FROM prescriptions WHERE id = $1`, antes.DraftID,
	).Scan(&studioID); err != nil {
		t.Fatal(err)
	}
	// Sessao ABERTA (finished_at nulo) com um aquecimento leve, mais recente que tudo.
	var sess string
	if err := database.QueryRow(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at)
		VALUES ($1, $2, gen_random_uuid(), now()) RETURNING id::text`,
		vitorID, studioID,
	).Scan(&sess); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, sess) })
	if _, err := database.Exec(`
		INSERT INTO workout_sets (session_id, exercise_id, client_set_id, set_index, reps, load_kg, performed_at)
		VALUES ($1, $2, gen_random_uuid(), 1, 12, 10,
		        (SELECT COALESCE(MAX(performed_at), now()) + interval '1 second' FROM workout_sets))`,
		sess, exID,
	); err != nil {
		t.Fatal(err)
	}

	clearTomorrow(t, database, []string{vitorID}, tomorrow)
	depois, err := svc.DraftFromLast(ctx, fredID, modelID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	if depois.Items[0].LoadKg != esperado {
		t.Fatalf("o aquecimento de 10 kg de uma sessao ABERTA virou a carga prescrita: %v (era %v)",
			depois.Items[0].LoadKg, esperado)
	}
}

// A fonte separa o que o CORPO levantou do que a ficha anterior so trazia.
//
// Sao dois degraus diferentes e o personal precisa dos dois separados: o corpo e FATO —
// aquela pessoa levantou aquilo; a ficha anterior e INTENCAO — o personal prescreveu e o
// corpo ainda nao executou. Carimbados iguais, o personal le "veio da ultima serie" num
// numero que ninguem nunca levantou, e a coluna para de fazer a unica coisa que ela existe
// para fazer: mostrar de onde veio o numero.
//
// O cenario: o Vitor tem serie executada num exercicio e nenhuma no outro que a ficha
// anterior dele traz. Os dois nao podem sair com o mesmo carimbo.
func TestFonteSeparaCorpoDeFichaAnterior(t *testing.T) {
	database := openSeeded(t)
	now, tomorrow := clockOnDBDay(t, database)
	svc := New(database, func() time.Time { return now })
	ctx := context.Background()
	fredID := personIDByPhone(t, database, seed.PhoneFred)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	clearTomorrow(t, database, []string{vitorID}, tomorrow)

	var studioID, executado string
	if err := database.QueryRow(`
		SELECT s.id::text, e.id::text
		FROM studios s
		JOIN people owner ON owner.id = s.owner_person_id
		JOIN exercises e ON e.studio_id = s.id AND e.name = 'Supino'
		WHERE owner.phone = $1`, seed.PhoneFred,
	).Scan(&studioID, &executado); err != nil {
		t.Fatal(err)
	}

	// Dois exercicios que o corpo do Vitor NUNCA tocou. Escolhidos por consulta, e nao a
	// dedo: os 15 pacotes de teste dividem o mesmo banco (ver Makefile) e o Vitor chega
	// aqui com serie executada que outra suite deixou.
	rows, err := database.Query(`
		SELECT e.id::text, e.name FROM exercises e
		WHERE e.studio_id = $1
		  AND e.id NOT IN (
		      SELECT ws.exercise_id FROM workout_sets ws
		      JOIN workout_sessions sess ON sess.id = ws.session_id
		      WHERE sess.person_id = $2)
		ORDER BY e.name LIMIT 2`, studioID, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	var virgens, nomes []string
	for rows.Next() {
		var id, nome string
		if err := rows.Scan(&id, &nome); err != nil {
			t.Fatal(err)
		}
		virgens = append(virgens, id)
		nomes = append(nomes, nome)
	}
	rows.Close()
	if len(virgens) < 2 {
		t.Fatal("a biblioteca nao tem dois exercicios sem serie executada do Vitor")
	}
	soPrescrito, soPrescrito2 := virgens[0], virgens[1]

	// Serie executada de 47,5 kg em Supino, sessao FECHADA. performed_at ancorado no
	// maximo existente pelo mesmo motivo da consulta acima: banco compartilhado.
	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at, finished_at)
		VALUES ($1, $2, gen_random_uuid(), now(), now())
		RETURNING id::text`, vitorID, studioID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, sessionID) })
	if _, err := database.Exec(`
		INSERT INTO workout_sets (session_id, exercise_id, client_set_id, set_index, reps, load_kg, performed_at)
		VALUES ($1, $2, gen_random_uuid(), 1, 8, 47.5,
		        (SELECT COALESCE(MAX(performed_at), now()) + interval '1 second' FROM workout_sets))`,
		sessionID, executado,
	); err != nil {
		t.Fatal(err)
	}

	// A ficha publicada de hoje passa a trazer os dois exercicios que o corpo nunca fez,
	// carimbados 'history' — que e exatamente a linha que a cascata escreve hoje ao
	// propagar o carimbo da ficha anterior. Os numeros (33 e 27) nao existem em nenhum
	// Modelo: se aparecerem no rascunho, vieram da ficha.
	naFicha := func(exID string, load float64) {
		t.Helper()
		var itemID string
		if err := database.QueryRow(`
			INSERT INTO prescription_items (
				prescription_id, exercise_id, position, planned_sets, planned_reps,
				load_kg, load_source
			)
			SELECT pr.id, $3::uuid,
			       (SELECT MAX(position) + 1 FROM prescription_items WHERE prescription_id = pr.id),
			       3, '8-12', $4, 'history'
			FROM prescriptions pr
			WHERE pr.person_id = $1 AND pr.studio_id = $2 AND pr.status = 'published'
			  AND pr.for_date = $5::date
			RETURNING id::text`,
			vitorID, studioID, exID, load, now.Format("2006-01-02"),
		).Scan(&itemID); err != nil {
			t.Fatal(err)
		}
		// Sem isto a ficha de hoje do Vitor fica com itens a mais para as outras suites.
		t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM prescription_items WHERE id = $1`, itemID) })
	}
	naFicha(soPrescrito, 33)
	naFicha(soPrescrito2, 27)

	// Um Modelo com os dois casos lado a lado: o que o corpo fez e o que so foi prescrito.
	modelo, err := svc.CreateModel(ctx, fredID, NewModel{
		Name: "Fonte de teste",
		Items: []NewModelItem{
			{ExerciseID: executado, PlannedSets: 3, PlannedReps: "8"},
			{ExerciseID: soPrescrito, PlannedSets: 3, PlannedReps: "8"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Nesta ordem: prescriptions.model_id e ON DELETE RESTRICT, entao o Modelo so sai
	// depois das fichas que nasceram dele. Ao contrario, ele fica no banco e a proxima
	// rodada bate no nome repetido.
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM prescriptions WHERE model_id = $1`, modelo.ID)
		_, _ = database.Exec(`DELETE FROM models WHERE id = $1`, modelo.ID)
	})

	draft, err := svc.DraftFromLast(ctx, fredID, modelo.ID, vitorID, "last")
	if err != nil {
		t.Fatal(err)
	}
	// O rascunho aponta para o Modelo com ON DELETE RESTRICT: sem apagar ele primeiro, a
	// limpeza do Modelo falha calada e a rodada seguinte esbarra no nome ja ocupado.
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM prescriptions WHERE id = $1`, draft.DraftID) })
	corpo, ficha := draft.Items[0], draft.Items[1]
	if corpo.LoadKg != 47.5 || ficha.LoadKg != 33 {
		t.Fatalf("cenario nao montou: executado %v kg (queria 47,5) e so prescrito %v kg (queria 33)",
			corpo.LoadKg, ficha.LoadKg)
	}
	if corpo.LoadSource != "history" {
		t.Fatalf("%s saiu como %q; ele TEM serie executada e so pode ser history",
			corpo.Name, corpo.LoadSource)
	}
	if ficha.LoadSource == corpo.LoadSource {
		t.Fatalf("%s (executado) e %s (so prescrito) sairam os dois como %q. O personal nao "+
			"tem como saber que o Vitor nunca levantou os %v kg de %s.",
			corpo.Name, ficha.Name, ficha.LoadSource, ficha.LoadKg, ficha.Name)
	}
	if ficha.LoadSource == "starter" {
		t.Fatalf("%s saiu como starter, mas 33 kg nao e chute de Modelo nenhum: o numero "+
			"veio da ficha anterior e o carimbo tem que dizer isso", ficha.Name)
	}

	// A mesma separacao vale na TROCA: exercicio que o corpo nunca fez e que so a ficha
	// anterior tinha nao pode chegar carimbado como serie executada.
	if err := svc.SwapItem(ctx, fredID, draft.DraftID, ficha.ID, soPrescrito2); err != nil {
		t.Fatal(err)
	}
	var trocada float64
	var fonte string
	if err := database.QueryRow(`
		SELECT load_kg, load_source FROM prescription_items WHERE id = $1`, ficha.ID,
	).Scan(&trocada, &fonte); err != nil {
		t.Fatal(err)
	}
	if trocada != 27 {
		t.Fatalf("%s saiu da troca com %v kg; o teste precisa dos 27 kg que so a ficha tem",
			nomes[1], trocada)
	}
	if fonte == corpo.LoadSource {
		t.Fatalf("%s entrou na troca carimbado %q, igual ao exercicio executado, sem o Vitor "+
			"nunca ter feito %s", nomes[1], fonte, nomes[1])
	}
}
