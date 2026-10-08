package places

import (
	"context"
	"errors"
	"testing"
)

type storeStub struct {
	query  Query
	slug   string
	locale string
	items  []Place
	err    error
}

func (s *storeStub) SearchPublic(_ context.Context, q Query) ([]Place, error) {
	s.query = q
	return s.items, s.err
}
func (s *storeStub) GetPublic(_ context.Context, slug, locale string) (Place, error) {
	s.slug, s.locale = slug, locale
	return Place{}, s.err
}

func TestSearchNormalizesAndPaginates(t *testing.T) {
	store := &storeStub{items: []Place{{Slug: "munich"}, {Slug: "paris"}, {Slug: "vienna"}}}
	service, _ := NewService(store)
	page, err := service.Search(context.Background(), Query{Search: "  ＭＵＮＩＣＨ  ", Locale: "zh-CN", Limit: 2, Offset: 4})
	if err != nil || store.query.Search != "munich" || store.query.Limit != 3 || store.query.Locale != "zh-CN" || len(page.Items) != 2 || page.NextOffset == nil || *page.NextOffset != 6 {
		t.Fatalf("page=%+v query=%+v err=%v", page, store.query, err)
	}
}
func TestInvalidQueriesNeverReachStore(t *testing.T) {
	for _, q := range []Query{{Locale: "xx", Limit: 20}, {Locale: "en", Limit: 0}, {Locale: "en", Limit: 51}, {Locale: "en", Limit: 20, Offset: -1}, {Locale: "en", Limit: 20, Offset: 10001}} {
		store := &storeStub{}
		service, _ := NewService(store)
		if _, err := service.Search(context.Background(), q); !errors.Is(err, ErrInvalidQuery) || store.query.Locale != "" {
			t.Fatalf("query=%+v err=%v", q, err)
		}
	}
}
func TestGetKeepsNotFoundIdentityAndStableSlug(t *testing.T) {
	store := &storeStub{err: ErrNotFound}
	service, _ := NewService(store)
	if _, err := service.Get(context.Background(), "munich", "de"); !errors.Is(err, ErrNotFound) || store.slug != "munich" || store.locale != "de" {
		t.Fatalf("err=%v store=%+v", err, store)
	}
	for _, slug := range []string{"", "../draft", "Munich", "münchen"} {
		if _, err := service.Get(context.Background(), slug, "en"); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("slug=%q err=%v", slug, err)
		}
	}
}
