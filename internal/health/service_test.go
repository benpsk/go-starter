package health

import (
	"context"
	"errors"
	"testing"
)

type repoStub struct {
	err error
}

func (r repoStub) Ping(context.Context) error { return r.err }

func TestStatus(t *testing.T) {
	cases := []struct {
		name       string
		repo       Repository
		wantStatus string
		wantDB     string
	}{
		{"healthy", repoStub{}, StatusOK, StatusOK},
		{"database down", repoStub{err: errors.New("connection refused")}, StatusDegraded, StatusDown},
		{"no repository", nil, StatusDegraded, StatusSkipped},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := NewService(tc.repo).Status(context.Background())
			if err != nil {
				t.Fatalf("Status must report degradation in the payload, not as an error: %v", err)
			}
			if status.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", status.Status, tc.wantStatus)
			}
			if status.Database.Status != tc.wantDB {
				t.Fatalf("database status = %q, want %q", status.Database.Status, tc.wantDB)
			}
			if status.CheckedAt.IsZero() {
				t.Fatalf("expected CheckedAt to be set")
			}
		})
	}
}

func TestStatusIncludesFailureMessage(t *testing.T) {
	status, _ := NewService(repoStub{err: errors.New("connection refused")}).Status(context.Background())
	if status.Database.Message == "" {
		t.Fatalf("expected the failure reason to be reported")
	}
}
