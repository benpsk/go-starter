package postgres

import (
	"context"
	"fmt"

	"github.com/benpsk/go-starter/internal/sample"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SampleStore struct {
	db DBTX
}

func NewSampleStore(pool *pgxpool.Pool) *SampleStore {
	return &SampleStore{db: pool}
}

func (s *SampleStore) List(ctx context.Context) ([]sample.Item, error) {
	rows, err := s.db.Query(ctx, `
		select id, name, created_at
		from sample_items
		order by id desc
	`)
	if err != nil {
		return nil, fmt.Errorf("list sample items: %w", err)
	}
	defer rows.Close()

	items := make([]sample.Item, 0)
	for rows.Next() {
		var item sample.Item
		if err := rows.Scan(&item.ID, &item.Name, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan sample item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sample items: %w", err)
	}

	return items, nil
}

func (s *SampleStore) Create(ctx context.Context, name string) (sample.Item, error) {
	var item sample.Item
	err := s.db.QueryRow(ctx, `
		insert into sample_items (name)
		values ($1)
		returning id, name, created_at
	`, name).Scan(&item.ID, &item.Name, &item.CreatedAt)
	if err != nil {
		return sample.Item{}, fmt.Errorf("create sample item: %w", err)
	}
	return item, nil
}
