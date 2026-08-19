package today

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

func TestProntidaoScore(t *testing.T) {
	if g := score(4, 2, 4); g != 80 {
		t.Fatalf("%d", g)
	}
	if label(80) != "Pode ir com carga" {
		t.Fatalf("%s", label(80))
	}
	if label(34) != "Versão leve" {
		t.Fatal(label(34))
	}
}

func TestProntidaoUpsert(t *testing.T) {
	db := openSeeded(t)
	svc := New(db, time.Now)
	vitorID := personIDByPhone(t, db, "+5511900000002")

	got, err := svc.PutProntidao(context.Background(), vitorID, 4, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 80 || got.Energy != 4 || got.Soreness != 2 || got.Sleep != 4 {
		t.Fatalf("%+v", got)
	}
	if got.Label != "Pode ir com carga" {
		t.Fatalf("%s", got.Label)
	}

	today, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if today.Prontidao.Score != 80 || today.Prontidao.Label != "Pode ir com carga" {
		t.Fatalf("today after save %+v", today.Prontidao)
	}

	got, err = svc.PutProntidao(context.Background(), vitorID, 1, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 20 || got.Label != "Versão leve" {
		t.Fatalf("second put %+v", got)
	}
}

func TestProntidaoUpsertRejectsOutOfRange(t *testing.T) {
	db := openSeeded(t)
	svc := New(db, time.Now)
	vitorID := personIDByPhone(t, db, "+5511900000002")
	if _, err := svc.PutProntidao(context.Background(), vitorID, 0, 3, 3); err != ErrProntidaoInvalid {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.PutProntidao(context.Background(), vitorID, 6, 3, 3); err != ErrProntidaoInvalid {
		t.Fatalf("got %v", err)
	}
}

func TestTodayLoadIsThePersons(t *testing.T) {
	db := openSeeded(t)
	svc := New(db, time.Now)
	vitorID := personIDByPhone(t, db, "+5511900000002")
	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prescription == nil || got.Prescription.Items[0].LoadKg != 40 {
		t.Fatalf("vitor load %+v", got.Prescription)
	}
}

func pgToday(t *testing.T, database *sql.DB) time.Time {
	t.Helper()
	var d time.Time
	if err := database.QueryRow(`SELECT current_date`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC)
}

func bondIDOf(t *testing.T, database *sql.DB, personID string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT active_bond_id::text FROM people WHERE id = $1`, personID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func restoreVitorOfensiva(t *testing.T, database *sql.DB, vitorID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM comebacks WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`, vitorID)
		_, _ = database.Exec(`
			DELETE FROM prescriptions
			WHERE person_id = $1 AND for_date <> current_date`,
			vitorID,
		)
		_ = seed.Dev(context.Background(), database)
		_, _ = database.Exec(`
			UPDATE streaks
			SET current_count = 0, protector_available = true,
			    protector_spent_at = NULL, last_fulfilled_on = NULL, updated_at = now()
			WHERE bond_id = (SELECT active_bond_id FROM people WHERE id = $1)`,
			vitorID,
		)
	})
}

func clonePublishedOn(t *testing.T, database *sql.DB, personID, forDate string) {
	t.Helper()
	var srcID string
	if err := database.QueryRow(`
		SELECT id::text FROM prescriptions
		WHERE person_id = $1 AND status = 'published'
		ORDER BY for_date DESC LIMIT 1`,
		personID,
	).Scan(&srcID); err != nil {
		t.Fatal(err)
	}
	var dstID string
	if err := database.QueryRow(`
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
		SELECT studio_id, person_id, model_id, $2::date, 'published', now()
		FROM prescriptions WHERE id = $1
		ON CONFLICT (person_id, studio_id, for_date) WHERE status = 'published'
		DO UPDATE SET updated_at = now()
		RETURNING id::text`,
		srcID, forDate,
	).Scan(&dstID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps,
			load_kg, rest_seconds, notes, load_source
		)
		SELECT $1, exercise_id, position, planned_sets, planned_reps,
		       load_kg, rest_seconds, notes, load_source
		FROM prescription_items WHERE prescription_id = $2
		ON CONFLICT (prescription_id, position) DO NOTHING`,
		dstID, srcID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestFirstMissSpendsProtector(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorOfensiva(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	clock := day
	svc := New(database, func() time.Time { return clock })

	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, day.AddDate(0, 0, -1).Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	clonePublishedOn(t, database, vitorID, day.AddDate(0, 0, -1).Format("2006-01-02"))

	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ofensiva.ProtectorAvailable || got.Ofensiva.CurrentCount != 4 {
		t.Fatalf("first today %+v", got.Ofensiva)
	}

	again, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Ofensiva.ProtectorAvailable || again.Ofensiva.CurrentCount != 4 {
		t.Fatalf("second today same day %+v", again.Ofensiva)
	}

	clock = day.AddDate(0, 0, 1)
	if _, err := database.Exec(`
		DELETE FROM prescriptions
		WHERE person_id = $1 AND for_date = $2::date AND status = 'published'`,
		vitorID, day.Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	next, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Ofensiva.ProtectorAvailable || next.Ofensiva.CurrentCount != 4 {
		t.Fatalf("day after first miss %+v", next.Ofensiva)
	}
	twice, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if twice.Ofensiva.ProtectorAvailable || twice.Ofensiva.CurrentCount != 4 {
		t.Fatalf("day after first miss twice %+v", twice.Ofensiva)
	}
}

func TestSecondMissZerosOfensiva(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorOfensiva(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	clock := day
	svc := New(database, func() time.Time { return clock })

	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, day.AddDate(0, 0, -1).Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	clonePublishedOn(t, database, vitorID, day.AddDate(0, 0, -1).Format("2006-01-02"))

	if _, err := svc.Today(context.Background(), vitorID); err != nil {
		t.Fatal(err)
	}

	clock = day.AddDate(0, 0, 1)
	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ofensiva.CurrentCount != 0 || got.Ofensiva.ProtectorAvailable {
		t.Fatalf("second miss %+v", got.Ofensiva)
	}
}

func TestD11OpensComebackWithoutWipingPR(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorOfensiva(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	svc := New(database, func() time.Time { return day })

	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, day.AddDate(0, 0, -11).Format("2006-01-02"),
	); err != nil {
		t.Fatal(err)
	}
	clonePublishedOn(t, database, vitorID, day.AddDate(0, 0, -1).Format("2006-01-02"))

	if _, err := database.Exec(`
		INSERT INTO personal_records (person_id, exercise_id, load_kg, reps)
		SELECT $1, e.id, 40, 8
		FROM exercises e
		JOIN studios s ON s.id = e.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $2 AND e.name = 'Supino'
		ON CONFLICT (person_id, exercise_id) DO UPDATE SET load_kg = EXCLUDED.load_kg`,
		vitorID, seed.PhoneFred,
	); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Comeback == nil {
		t.Fatal("comeback missing")
	}
	if got.Comeback.Minutes != 9 {
		t.Fatalf("minutes %d", got.Comeback.Minutes)
	}
	if got.Comeback.CoachLine != "Sua carga e seus recordes continuam aí." {
		t.Fatalf("coach_line %q", got.Comeback.CoachLine)
	}

	var prs int
	if err := database.QueryRow(`SELECT count(*) FROM personal_records WHERE person_id = $1`, vitorID).Scan(&prs); err != nil {
		t.Fatal(err)
	}
	if prs < 1 {
		t.Fatal("personal_records wiped")
	}
}

func TestCompleteComebackTwiceSucceeds(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	joseID := personIDByPhone(t, database, seed.PhoneJose)
	bondID := bondIDOf(t, database, joseID)
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM comebacks WHERE bond_id = $1`, bondID)
		_, _ = database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'retomada'`, joseID)
	})

	var comebackID string
	if err := database.QueryRow(`
		INSERT INTO comebacks (bond_id, missed_on)
		VALUES ($1, current_date - 3)
		ON CONFLICT (bond_id, missed_on) DO UPDATE SET completed_at = NULL
		RETURNING id::text`,
		bondID,
	).Scan(&comebackID); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := svc.CompleteComeback(ctx, joseID, comebackID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteComeback(ctx, joseID, comebackID); err != nil {
		t.Fatalf("second complete: %v", err)
	}

	var completed sql.NullTime
	if err := database.QueryRow(`SELECT completed_at FROM comebacks WHERE id = $1`, comebackID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if !completed.Valid {
		t.Fatal("completed_at empty")
	}
}

func TestCompleteComebackAwardsRetomadaBadge(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	joseID := personIDByPhone(t, database, seed.PhoneJose)
	bondID := bondIDOf(t, database, joseID)
	if _, err := database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'retomada'`, joseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM comebacks WHERE bond_id = $1`, bondID)
		_, _ = database.Exec(`DELETE FROM badges WHERE person_id = $1 AND badge_key = 'retomada'`, joseID)
	})

	var comebackID string
	if err := database.QueryRow(`
		INSERT INTO comebacks (bond_id, missed_on)
		VALUES ($1, current_date - 4)
		ON CONFLICT (bond_id, missed_on) DO UPDATE SET completed_at = NULL
		RETURNING id::text`,
		bondID,
	).Scan(&comebackID); err != nil {
		t.Fatal(err)
	}

	if err := svc.CompleteComeback(context.Background(), joseID, comebackID); err != nil {
		t.Fatal(err)
	}

	var key string
	if err := database.QueryRow(`
		SELECT badge_key FROM badges
		WHERE person_id = $1 AND badge_key = 'retomada'`,
		joseID,
	).Scan(&key); err != nil {
		t.Fatalf("retomada row: %v", err)
	}
}

// A carga de referencia sai da serie EXECUTADA, nunca da prescrita.
//
// A distincao e o ponto todo: o personal prescreve 40 e a pessoa pode ter levantado 47,5.
// Se este teste passar lendo a prescricao, a tela mostra ao aluno o que mandaram ele fazer
// e chama isso de "o que voce fez". Por isso o peso executado aqui e de proposito diferente
// do prescrito — se alguem trocar a fonte por prescription_items, o numero 40 aparece e o
// teste cai.
func TestLastLoadComesFromTheExecutedSetNotThePrescribed(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	ctx := context.Background()
	vitorID := personIDByPhone(t, database, "+5511900000002")

	antes, err := svc.Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if antes.Prescription == nil || len(antes.Prescription.Items) == 0 {
		t.Fatal("vitor sem ficha de hoje")
	}
	primeiro := antes.Prescription.Items[0]
	if primeiro.LoadKg != 40 {
		t.Fatalf("prescrito mudou, o teste perdeu o contraste: %v", primeiro.LoadKg)
	}

	var studioID string
	if err := database.QueryRow(
		`SELECT studio_id::text FROM bonds WHERE id = (SELECT active_bond_id FROM people WHERE id = $1)`,
		vitorID,
	).Scan(&studioID); err != nil {
		t.Fatal(err)
	}

	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO workout_sessions (person_id, studio_id, client_id, started_at, finished_at)
		VALUES ($1, $2, gen_random_uuid(), now(), now())
		RETURNING id::text`,
		vitorID, studioID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, sessionID) })

	// performed_at calculado a partir do MAXIMO ja existente, nao de now(). Os 15 pacotes
	// de teste dividem o mesmo DATABASE_URL (ver Makefile) e o Vitor ja chega aqui com
	// sessao terminada de outra suite: ancorar em now() deixava a corrida decidir qual
	// linha o DISTINCT ON escolhe, e o teste passava ou caia por sorte.
	if _, err := database.Exec(`
		INSERT INTO workout_sets (session_id, exercise_id, client_set_id, set_index, reps, load_kg, performed_at)
		VALUES ($1, $2, gen_random_uuid(), 1, 9, 47.5,
		        (SELECT COALESCE(MAX(performed_at), now()) + interval '1 second' FROM workout_sets))`,
		sessionID, primeiro.ExerciseID,
	); err != nil {
		t.Fatal(err)
	}

	depois, err := svc.Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	item := depois.Prescription.Items[0]
	if item.LastKg == nil {
		t.Fatal("last_kg veio nil, mas a serie executada existe")
	}
	if *item.LastKg != 47.5 {
		t.Fatalf("last_kg = %v, queria 47.5 (o executado), nao %v (o prescrito)", *item.LastKg, item.LoadKg)
	}
	if item.LastReps == nil || *item.LastReps != 9 {
		t.Fatalf("last_reps = %v, queria 9", item.LastReps)
	}
	if item.LoadKg != 40 {
		t.Fatalf("a prescricao foi contaminada: load_kg = %v, tinha que continuar 40", item.LoadKg)
	}
}

// Sem historico naquele exercicio, a ausencia tem que ser DIZIVEL: nil, para a tela
// desenhar o traco. Numero inventado e pior que numero ausente.
func TestLastLoadIsNilWhenTheBodyNeverDidIt(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, time.Now)
	joseID := personIDByPhone(t, database, "+5511900000004")

	got, err := svc.Today(context.Background(), joseID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prescription == nil || len(got.Prescription.Items) == 0 {
		t.Fatal("jose sem ficha de hoje")
	}
	for _, it := range got.Prescription.Items {
		if it.LastKg != nil {
			t.Fatalf("%s veio com last_kg %v, mas jose nao tem sessao terminada", it.Name, *it.LastKg)
		}
	}
}

// A Retomada nasce na PRIMEIRA falta, nao no decimo primeiro dia.
//
// O caminho antigo era openD11Comeback, que exige `calendarDays(anchor, now) >= 11`, ou o
// personal aplicar um student_stopped da fila. Este teste nao espera 11 dias: falta UMA
// vez e cobra a Retomada no mesmo Today. Se alguem tirar o upsert de applyYesterdayMiss,
// comeback vem nil e isto cai.
func TestComebackIsBornOnTheFirstMiss(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	restoreVitorOfensiva(t, database, vitorID)
	bondID := bondIDOf(t, database, vitorID)

	day := pgToday(t, database)
	svc := New(database, func() time.Time { return day })
	ontem := day.AddDate(0, 0, -1).Format("2006-01-02")

	if _, err := database.Exec(`DELETE FROM comebacks WHERE bond_id = $1`, bondID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE streaks
		SET current_count = 4, protector_available = true,
		    protector_spent_at = NULL, last_fulfilled_on = $2::date, updated_at = now()
		WHERE bond_id = $1`,
		bondID, ontem,
	); err != nil {
		t.Fatal(err)
	}
	// Ficha publicada ontem e nenhuma sessao terminada: e isto que define a falta.
	clonePublishedOn(t, database, vitorID, ontem)

	got, err := svc.Today(context.Background(), vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Comeback == nil {
		t.Fatal("faltou UMA vez e a Retomada nao apareceu: comeback nil")
	}

	// O Protetor continua fazendo o que promete: a primeira falta nao zera a Ofensiva.
	// Retomada nao e castigo, e por isso as duas coisas convivem.
	if got.Ofensiva.CurrentCount != 4 {
		t.Fatalf("a primeira falta zerou a Ofensiva: %+v", got.Ofensiva)
	}

	var aberta int
	if err := database.QueryRow(
		`SELECT count(*) FROM comebacks WHERE bond_id = $1 AND completed_at IS NULL`, bondID,
	).Scan(&aberta); err != nil {
		t.Fatal(err)
	}
	if aberta != 1 {
		t.Fatalf("comebacks abertas = %d, queria exatamente 1", aberta)
	}

	// Segunda leitura no mesmo dia nao pode abrir outra: o UNIQUE (bond_id, missed_on) e o
	// guarda, e sem esta checagem uma falta viraria uma Retomada por abertura de tela.
	if _, err := svc.Today(context.Background(), vitorID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT count(*) FROM comebacks WHERE bond_id = $1 AND completed_at IS NULL`, bondID,
	).Scan(&aberta); err != nil {
		t.Fatal(err)
	}
	if aberta != 1 {
		t.Fatalf("abrir Hoje duas vezes criou %d Retomadas", aberta)
	}
}

// Cumprimento e a Sessao do dia FECHADA, e nada alem disso.
//
// O teste cobra as duas pontas porque e exatamente aqui que a definicao escorrega: sessao
// aberta nao cumpre (senao bastaria abrir o app na academia), e sessao fechada cumpre
// mesmo sem nenhuma serie gravada — Cumprimento nao e volume, nao e carga e nao e
// percentual do prescrito. Ver o verbete no CONTEXT.md.
func TestCumprimentoIsTheClosedSessionNotTheOpenOne(t *testing.T) {
	database := openSeeded(t)
	vitorID := personIDByPhone(t, database, seed.PhoneVitor)
	day := pgToday(t, database)
	svc := New(database, func() time.Time { return day })
	ctx := context.Background()

	var prID, studioID string
	if err := database.QueryRow(`
		SELECT pr.id::text, pr.studio_id::text FROM prescriptions pr
		WHERE pr.person_id = $1 AND pr.for_date = current_date AND pr.status = 'published'`,
		vitorID,
	).Scan(&prID, &studioID); err != nil {
		t.Fatal(err)
	}

	antes, err := svc.Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if antes.Cumprido {
		t.Fatal("cumprido=true antes de qualquer sessao terminada")
	}

	var sessionID string
	if err := database.QueryRow(`
		INSERT INTO workout_sessions (person_id, studio_id, prescription_id, client_id, started_at)
		VALUES ($1, $2, $3, gen_random_uuid(), now())
		RETURNING id::text`,
		vitorID, studioID, prID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM workout_sessions WHERE id = $1`, sessionID) })

	aberta, err := svc.Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if aberta.Cumprido {
		t.Fatal("sessao ABERTA cumpriu o dia; bastaria abrir o app para a Ofensiva contar")
	}

	if _, err := database.Exec(
		`UPDATE workout_sessions SET finished_at = now() WHERE id = $1`, sessionID,
	); err != nil {
		t.Fatal(err)
	}
	fechada, err := svc.Today(ctx, vitorID)
	if err != nil {
		t.Fatal(err)
	}
	if !fechada.Cumprido {
		t.Fatal("sessao FECHADA nao cumpriu o dia")
	}
}

// Marco de calendario e dia que a pessoa ja trata como recomeco. Nao precisa de banco:
// e funcao pura, e por isso este teste roda em qualquer fuso e em qualquer dia.
func TestMarcoDeCalendario(t *testing.T) {
	casos := []struct {
		dia    string
		quer   bool
		porque string
	}{
		{"2026-08-17", true, "segunda"},
		{"2026-09-01", true, "dia 1º (e terça: o 1º vale sozinho)"},
		{"2026-08-19", false, "quarta no meio do mês"},
		{"2026-08-22", false, "sábado"},
		{"2026-08-23", false, "domingo não é recomeço aqui; a semana começa na segunda"},
		{"2026-06-01", true, "dia 1º caindo na segunda: os dois ao mesmo tempo"},
	}
	for _, c := range casos {
		d, err := time.Parse("2006-01-02", c.dia)
		if err != nil {
			t.Fatal(err)
		}
		if got := marcoDeCalendario(d); got != c.quer {
			t.Errorf("%s (%s): marcoDeCalendario = %v, queria %v", c.dia, c.porque, got, c.quer)
		}
	}
}
