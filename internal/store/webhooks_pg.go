package store

import (
	"context"
	"fmt"

	"github.com/lyonwang/agentfleet/internal/model"
)

// ---- webhooks（I7）PostgresStore ----

func (p *PostgresStore) CreateWebhook(ctx context.Context, w *model.Webhook) (int64, error) {
	if w.Events == nil {
		w.Events = []string{"*"}
	}
	err := p.pool.QueryRow(ctx,
		`INSERT INTO webhooks(name, url, secret, events, active) VALUES ($1,$2,$3,jsonb($4::text),$5)
		 RETURNING id, created_at`,
		w.Name, w.URL, w.Secret, jsonb(w.Events), w.Active).Scan(&w.ID, &w.CreatedAt)
	return w.ID, err
}

func (p *PostgresStore) ListWebhooks(ctx context.Context) ([]model.Webhook, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, name, url, secret, COALESCE(events,'["*"]'::jsonb), active, created_at FROM webhooks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Webhook{}
	for rows.Next() {
		w := model.Webhook{}
		var events []byte
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Secret, &events, &w.Active, &w.CreatedAt); err != nil {
			return nil, err
		}
		unjsonb(events, &w.Events)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *PostgresStore) DeleteWebhook(ctx context.Context, id int64) error {
	ct, err := p.pool.Exec(ctx, `DELETE FROM webhooks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("webhook %d: %w", id, ErrNotFound)
	}
	return nil
}
