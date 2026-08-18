package owner

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

type Home struct {
	Greeting      string      `json:"greeting"`
	StudentCount  int         `json:"student_count"`
	Fio           Fio         `json:"fio"`
	Attention     []Attention `json:"attention"`
	UnreadReturns int         `json:"unread_returns"`
}

type Fio struct {
	Prescribed int      `json:"prescribed"`
	Done       int      `json:"done"`
	Week       []FioDay `json:"week"`
}

type FioDay struct {
	ForDate    string `json:"for_date"`
	Done       int    `json:"done"`
	Prescribed int    `json:"prescribed"`
}

type Attention struct {
	ID       string `json:"id"`
	PersonID string `json:"person_id"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Decision string `json:"decision"`
	Rank     int    `json:"rank"`
}

func (s *Service) Home(ctx context.Context, personID string) (*Home, error) {
	day := s.now().Format("2006-01-02")

	var name, studioID string
	err := s.db.QueryRowContext(ctx, `
		SELECT p.name, b.studio_id::text
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		WHERE p.id = $1`,
		personID,
	).Scan(&name, &studioID)
	if err != nil {
		return nil, fmt.Errorf("owner home person: %w", err)
	}

	out := Home{
		Greeting:      fmt.Sprintf("Bom dia, %s", name),
		UnreadReturns: 0,
		Attention:     []Attention{},
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE studio_id = $1 AND role = 'student' AND status = 'active'`,
		studioID,
	).Scan(&out.StudentCount); err != nil {
		return nil, fmt.Errorf("owner home students: %w", err)
	}

	attention, err := s.loadAttention(ctx, studioID, day)
	if err != nil {
		return nil, err
	}
	out.Attention = attention

	fio, err := s.loadFio(ctx, studioID, day)
	if err != nil {
		return nil, err
	}
	out.Fio = fio
	return &out, nil
}

func (s *Service) loadAttention(ctx context.Context, studioID, day string) ([]Attention, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id::text, a.person_id::text, p.name, a.reason, a.rank
		FROM attention_items a
		JOIN people p ON p.id = a.person_id
		WHERE a.studio_id = $1 AND a.for_date = $2
		ORDER BY a.rank
		LIMIT 3`,
		studioID, day,
	)
	if err != nil {
		return nil, fmt.Errorf("owner attention: %w", err)
	}
	defer rows.Close()

	items := []Attention{}
	for rows.Next() {
		var it Attention
		if err := rows.Scan(&it.ID, &it.PersonID, &it.Name, &it.Reason, &it.Rank); err != nil {
			return nil, fmt.Errorf("owner attention scan: %w", err)
		}
		it.Decision = decisionFor(it.Reason)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Service) loadFio(ctx context.Context, studioID, today string) (Fio, error) {
	now := s.now()
	mon, sun := isoWeekBounds(now)

	rows, err := s.db.QueryContext(ctx, `
		SELECT for_date::text,
		       count(*) AS prescribed,
		       count(*) FILTER (WHERE status = 'done') AS done
		FROM fio_days
		WHERE studio_id = $1 AND for_date BETWEEN $2 AND $3
		GROUP BY for_date`,
		studioID, mon.Format("2006-01-02"), sun.Format("2006-01-02"),
	)
	if err != nil {
		return Fio{}, fmt.Errorf("owner fio: %w", err)
	}
	defer rows.Close()

	byDay := map[string]FioDay{}
	for rows.Next() {
		var d FioDay
		if err := rows.Scan(&d.ForDate, &d.Prescribed, &d.Done); err != nil {
			return Fio{}, fmt.Errorf("owner fio scan: %w", err)
		}
		byDay[d.ForDate] = d
	}
	if err := rows.Err(); err != nil {
		return Fio{}, err
	}

	week := make([]FioDay, 0, 7)
	for d := mon; !d.After(sun); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if hit, ok := byDay[key]; ok {
			week = append(week, hit)
			continue
		}
		week = append(week, FioDay{ForDate: key})
	}

	todayFio := byDay[today]
	return Fio{
		Prescribed: todayFio.Prescribed,
		Done:       todayFio.Done,
		Week:       week,
	}, nil
}

func isoWeekBounds(t time.Time) (time.Time, time.Time) {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	mon := t.AddDate(0, 0, 1-wd)
	return mon, mon.AddDate(0, 0, 6)
}

func decisionFor(reason string) string {
	switch reason {
	case "student_stopped":
		return "Mandar a retomada de 9 minutos"
	case "pain_flag":
		return "Ligar antes de publicar"
	case "debut":
		return "Publicar o treino curto"
	case "high_effort":
		return "Manter carga"
	default:
		return ""
	}
}
