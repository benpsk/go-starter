package postgres

import (
	"testing"

	"github.com/benpsk/go-starter/internal/sample"
)

func TestSampleStoreCreateAndList(t *testing.T) {
	ctx, cleanup := withTx(t)
	defer cleanup()

	store := &SampleStore{db: DBFromContext(ctx, integrationPool)}
	service := sample.NewService(store)

	created, err := service.Create(ctx, "  first item  ")
	if err != nil {
		t.Fatalf("create sample item: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected created item id")
	}
	if created.Name != "first item" {
		t.Fatalf("expected trimmed name, got %q", created.Name)
	}

	items, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list sample items: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected at least one item")
	}
	if items[0].ID != created.ID {
		t.Fatalf("expected newest item first, got id=%d want=%d", items[0].ID, created.ID)
	}
}
