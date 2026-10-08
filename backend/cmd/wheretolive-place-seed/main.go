// Command wheretolive-place-seed creates test place fixtures through the platform API.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	"golang.org/x/text/language"
)

//go:embed test-places.json
var candidateData []byte

type candidate struct {
	Slug        string                    `json:"slug"`
	Name        string                    `json:"name"`
	Type        generated.PublicPlaceType `json:"type"`
	CountryCode string                    `json:"countryCode"`
	ParentSlug  *string                   `json:"parentSlug"`
}

func main() {
	base := flag.String("base-url", "http://127.0.0.1:8080", "server origin (HTTPS, or loopback HTTP)")
	apply := flag.Bool("apply", false, "create missing test drafts; default only previews")
	reason := flag.String("reason", "", "mandatory audit reason with --apply")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	if err := run(ctx, *base, os.Getenv("WHERETOLIVE_SEED_USERNAME"), os.Getenv("WHERETOLIVE_SEED_PASSWORD"), *reason, *apply, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "wheretolive-place-seed: %v\n", err)
		os.Exit(1)
	}
}

func loadCandidates() ([]candidate, error) {
	var items []candidate
	if err := json.Unmarshal(candidateData, &items); err != nil {
		return nil, fmt.Errorf("decode embedded catalogue: %w", err)
	}
	seen := map[string]candidate{}
	slugPattern := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	for _, item := range items {
		region, err := language.ParseRegion(item.CountryCode)
		if err != nil || !region.IsCountry() || len(item.CountryCode) != 2 || region.String() != item.CountryCode || !slugPattern.MatchString(item.Slug) || len(item.Slug) > 120 || strings.TrimSpace(item.Name) == "" || utf8.RuneCountInString(item.Name) > 200 {
			return nil, fmt.Errorf("invalid candidate %q", item.Slug)
		}
		if _, exists := seen[item.Slug]; exists {
			return nil, fmt.Errorf("duplicate candidate %s", item.Slug)
		}
		switch item.Type {
		case generated.Country:
			if item.ParentSlug != nil {
				return nil, fmt.Errorf("country %s has a parent", item.Slug)
			}
		case generated.City:
			if item.ParentSlug == nil {
				return nil, fmt.Errorf("city %s lacks a parent", item.Slug)
			}
			parent, ok := seen[*item.ParentSlug]
			if !ok || parent.Type != generated.Country || parent.CountryCode != item.CountryCode {
				return nil, fmt.Errorf("invalid parent for %s", item.Slug)
			}
		case generated.District, generated.Island, generated.Region:
			return nil, fmt.Errorf("unsupported candidate type for %s", item.Slug)
		default:
			return nil, fmt.Errorf("unsupported candidate type for %s", item.Slug)
		}
		seen[item.Slug] = item
	}
	if len(items) < 30 || len(items) > 50 {
		return nil, fmt.Errorf("catalogue must contain 30–50 candidates")
	}
	return items, nil
}

type apiClient struct {
	base  string
	http  *http.Client
	token string
	csrf  string
}

type statusError int

func (s statusError) Error() string { return fmt.Sprintf("platform API returned HTTP %d", s) }

func newClient(base string) (*apiClient, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("base-url must be an origin without credentials, path, query or fragment")
	}
	ip := net.ParseIP(parsed.Hostname())
	loopback := parsed.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopback) {
		return nil, fmt.Errorf("base-url requires HTTPS except on loopback")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create session jar: %w", err)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return &apiClient{base: strings.TrimSuffix(base, "/"), http: client}, nil
}

func (c *apiClient) request(ctx context.Context, method, path string, input, output any, expected int) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode platform request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api"+path, body)
	if err != nil {
		return fmt.Errorf("prepare platform request")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("platform request failed (connection, timeout or cancellation)")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != expected {
		return statusError(response.StatusCode)
	}
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(output); err != nil {
			return fmt.Errorf("invalid platform response")
		}
	}
	return nil
}

// find resolves exact slugs after bounded prefix search, including pagination.
func (c *apiClient) find(ctx context.Context, slug string) (*generated.ManagedPlace, error) {
	return c.search(ctx, url.Values{"q": {slug}}, func(item generated.ManagedPlace) bool { return item.Slug == slug })
}

func (c *apiClient) findCountry(ctx context.Context, code string) (*generated.ManagedPlace, error) {
	return c.search(ctx, url.Values{"countryCode": {code}}, func(item generated.ManagedPlace) bool {
		return item.Type == generated.Country && item.CountryCode == code
	})
}

func (c *apiClient) search(ctx context.Context, query url.Values, matches func(generated.ManagedPlace) bool) (*generated.ManagedPlace, error) {
	offset := 0
	for {
		query.Set("limit", "50")
		query.Set("offset", fmt.Sprint(offset))
		var page generated.ManagedPlacePage
		if err := c.request(ctx, http.MethodGet, "/platform/places?"+query.Encode(), nil, &page, http.StatusOK); err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if matches(item) {
				return &item, nil
			}
		}
		if page.NextOffset == nil {
			return nil, nil
		}
		if *page.NextOffset <= offset || *page.NextOffset > 10000 {
			return nil, fmt.Errorf("invalid catalogue pagination")
		}
		offset = *page.NextOffset
	}
}

func compatible(item candidate, existing *generated.ManagedPlace, parents map[string]*generated.ManagedPlace) bool {
	if (existing.Slug != item.Slug && item.Type != generated.Country) || existing.Type != item.Type || existing.CountryCode != item.CountryCode || existing.Id == uuid.Nil {
		return false
	}
	if item.ParentSlug == nil {
		return existing.ParentId == nil
	}
	parent := parents[*item.ParentSlug]
	return parent != nil && existing.ParentId != nil && *existing.ParentId == parent.Id
}

func run(ctx context.Context, base, username, password, reason string, apply bool, output io.Writer) (resultErr error) {
	if strings.TrimSpace(username) == "" || password == "" {
		return fmt.Errorf("WHERETOLIVE_SEED_USERNAME and WHERETOLIVE_SEED_PASSWORD are required")
	}
	reason = strings.TrimSpace(reason)
	if apply && (reason == "" || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 500 || strings.ContainsRune(reason, 0)) {
		return fmt.Errorf("--apply requires an audit reason of 1–500 characters")
	}
	items, err := loadCandidates()
	if err != nil {
		return err
	}
	client, err := newClient(base)
	if err != nil {
		return err
	}
	var tokens generated.AccessTokenResponse
	if err := client.request(ctx, http.MethodPost, "/platform/auth/login", generated.PlatformLoginRequest{Username: username, Password: &password}, &tokens, http.StatusOK); err != nil {
		return fmt.Errorf("platform login: %w", err)
	}
	client.token, client.csrf = tokens.AccessToken, tokens.CsrfToken
	// Revoke the dedicated session on failure and cancellation as well as success.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := client.request(cleanupCtx, http.MethodPost, "/platform/auth/logout", nil, nil, http.StatusNoContent); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("revoke import session: %w", err))
		}
	}()
	existing := map[string]*generated.ManagedPlace{}
	missing := 0
	// Detect every existing geography collision before the first write. Operator
	// names, metadata, coverage and publication are preserved on reruns.
	for _, item := range items {
		found, err := client.find(ctx, item.Slug)
		if err == nil && found == nil && item.Type == generated.Country {
			found, err = client.findCountry(ctx, item.CountryCode)
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", item.Slug, err)
		}
		if found != nil && !compatible(item, found, existing) {
			return fmt.Errorf("conflicting geography at %s; no import writes made", item.Slug)
		}
		existing[item.Slug] = found
		if found == nil {
			missing++
		}
	}
	if _, err := fmt.Fprintf(output, "Test places: %d; existing: %d; missing: %d\n", len(items), len(items)-missing, missing); err != nil {
		return fmt.Errorf("write preview: %w", err)
	}
	if !apply {
		for _, item := range items {
			if existing[item.Slug] == nil {
				if _, err := fmt.Fprintf(output, "create draft: %s (%s)\n", item.Slug, item.CountryCode); err != nil {
					return fmt.Errorf("write preview: %w", err)
				}
			}
		}
		return nil
	}
	created, skipped := 0, 0
	for _, item := range items {
		if existing[item.Slug] != nil {
			skipped++
			continue
		}
		request := generated.CreatePlatformPlaceRequest{Slug: item.Slug, Type: item.Type, CountryCode: item.CountryCode, Reason: reason, Details: generated.PlaceDetails{Name: item.Name, Languages: []string{}, Aliases: []generated.PlaceAlias{}}}
		if item.ParentSlug != nil {
			parentID := existing[*item.ParentSlug].Id
			request.ParentId = &parentID
		}
		var entry generated.ManagedPlace
		err := client.request(ctx, http.MethodPost, "/platform/places", request, &entry, http.StatusCreated)
		var status statusError
		if errors.As(err, &status) && status == statusError(http.StatusConflict) {
			found, findErr := client.find(ctx, item.Slug)
			if findErr == nil && found == nil && item.Type == generated.Country {
				found, findErr = client.findCountry(ctx, item.CountryCode)
			}
			if findErr != nil {
				return fmt.Errorf("resolve concurrent create %s: %w", item.Slug, findErr)
			}
			if found != nil && compatible(item, found, existing) {
				entry = *found
				err = nil
				skipped++
			} else {
				return fmt.Errorf("concurrent geography conflict at %s; rerun after review", item.Slug)
			}
		} else if err == nil {
			created++
		}
		if err != nil {
			return fmt.Errorf("create %s after %d creations: %w; rerun to resume", item.Slug, created, err)
		}
		existing[item.Slug] = &entry
	}
	_, err = fmt.Fprintf(output, "Created drafts: %d; preserved existing: %d\n", created, skipped)
	return err
}
