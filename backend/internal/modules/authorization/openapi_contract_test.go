package authorization

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestTenantBearerOperationsDeclareKnownPermission(t *testing.T) {
	document, err := os.ReadFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(document, &root); err != nil {
		t.Fatal(err)
	}
	paths := root["paths"].(map[string]any)
	for path, rawPath := range paths {
		if strings.HasPrefix(path, "/auth/") || path == "/authorization/permissions" || path == "/users/me" {
			continue
		}
		operations := rawPath.(map[string]any)
		for method, rawOperation := range operations {
			if method == "parameters" {
				continue
			}
			operation := rawOperation.(map[string]any)
			if !usesTenantBearer(operation["security"]) {
				continue
			}
			extension, ok := operation["x-modura-permission"].(map[string]any)
			if !ok {
				t.Errorf("%s %s has tenant bearer auth without x-modura-permission", method, path)
				continue
			}
			permission := Permission{Resource: Resource(extension["resource"].(string)), Action: Action(extension["action"].(string))}
			if !IsKnownPermission(permission) {
				t.Errorf("%s %s maps unknown permission %+v", method, path, permission)
			}
		}
	}
}

func usesTenantBearer(raw any) bool {
	security, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range security {
		entry, ok := item.(map[string]any)
		if ok {
			if _, exists := entry["bearerAuth"]; exists {
				return true
			}
		}
	}
	return false
}
