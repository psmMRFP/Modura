package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

type recordStoreStub struct{}

func (recordStoreStub) Record(context.Context, pgx.Tx, Event) error                 { return nil }
func (recordStoreStub) RecordPlatform(context.Context, pgx.Tx, PlatformEvent) error { return nil }
func (recordStoreStub) RecordTenantScopedPlatform(context.Context, pgx.Tx, TenantScopedPlatformEvent) error {
	return nil
}

type queryStoreStub struct {
	records    []Record
	event      Record
	eventError error
}

func (s queryStoreStub) List(context.Context, Query) ([]Record, error) { return s.records, nil }

func (s queryStoreStub) Get(context.Context, identity.TenantID, string) (Record, error) {
	return s.event, s.eventError
}

func (s queryStoreStub) ListPlatform(_ context.Context, _ PlatformQuery) ([]Record, error) {
	return s.records, nil
}

func TestListRedactsSensitiveStateRecursively(t *testing.T) {
	service, err := NewService(recordStoreStub{}, func(time.Time) (string, error) { return "id", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnableQueries(queryStoreStub{records: []Record{{BeforeState: []byte(`{"profile":{"name":"Ada","refreshToken":"secret"},"session_id":"hidden"}`)}}}); err != nil {
		t.Fatal(err)
	}
	records, err := service.List(context.Background(), identity.TenantID("tenant"), "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	state := string(records[0].BeforeState)
	if strings.Contains(state, "secret") || strings.Contains(state, "hidden") || !strings.Contains(state, "Ada") || strings.Count(state, "[redacted]") != 2 {
		t.Fatalf("redacted state = %s", state)
	}
}

func TestGetRedactsAndValidatesScope(t *testing.T) {
	service, err := NewService(recordStoreStub{}, func(time.Time) (string, error) { return "id", nil })
	if err != nil {
		t.Fatal(err)
	}
	stub := queryStoreStub{event: Record{AfterState: []byte(`{"passwordHash":"secret","status":"disabled"}`)}}
	if err := service.EnableQueries(stub); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), identity.TenantID("tenant"), ""); err == nil {
		t.Fatal("empty event id accepted")
	}
	record, err := service.Get(context.Background(), identity.TenantID("tenant"), "018bcfe5-6800-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state := string(record.AfterState)
	if strings.Contains(state, "secret") || !strings.Contains(state, "[redacted]") || !strings.Contains(state, "disabled") {
		t.Fatalf("redacted state = %s", state)
	}
}

func TestListPlatformValidatesBoundsAndRedacts(t *testing.T) {
	service, err := NewService(recordStoreStub{}, func(time.Time) (string, error) { return "id", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnableQueries(queryStoreStub{records: []Record{{BeforeState: []byte(`{"clientSecret":"secret"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListPlatform(context.Background(), PlatformQuery{Limit: 0}); err == nil {
		t.Fatal("unbounded platform query accepted")
	}
	if _, err := service.ListPlatform(context.Background(), PlatformQuery{Limit: 101}); err == nil {
		t.Fatal("oversized platform query accepted")
	}
	records, err := service.ListPlatform(context.Background(), PlatformQuery{Limit: 50, Action: "tenant.provisioned"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(records[0].BeforeState), "[redacted]") {
		t.Fatalf("platform state not redacted: %s", records[0].BeforeState)
	}
}
