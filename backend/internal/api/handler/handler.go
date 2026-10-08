// Package handler composes module-owned HTTP adapters into the generated contract.
package handler

import (
	"context"

	"github.com/modura-dev/modura/backend/internal/api/generated"
	apihttp "github.com/modura-dev/modura/backend/internal/api/transport"
	audithttp "github.com/modura-dev/modura/backend/internal/modules/audit/transport/http"
	authorizationhttp "github.com/modura-dev/modura/backend/internal/modules/authorization/transport/http"
	identityhttp "github.com/modura-dev/modura/backend/internal/modules/identity/transport/http"
	organizationhttp "github.com/modura-dev/modura/backend/internal/modules/organization/transport/http"
	placeshttp "github.com/modura-dev/modura/backend/internal/modules/places/transport/http"
	platformadminhttp "github.com/modura-dev/modura/backend/internal/modules/platformadmin/transport/http"
	platformtenanthttp "github.com/modura-dev/modura/backend/internal/modules/platformtenant/transport/http"
	provisioninghttp "github.com/modura-dev/modura/backend/internal/modules/provisioning/transport/http"
	settingshttp "github.com/modura-dev/modura/backend/internal/modules/settings/transport/http"
)

// Dependencies are the application capabilities required by HTTP delivery.
type Dependencies struct {
	PlatformPlaces   placeshttp.PlatformService
	Places           placeshttp.Service
	Identity         identityhttp.Service
	Authorizer       organizationhttp.Authorizer
	Authorization    authorizationhttp.Service
	Organization     organizationhttp.Service
	PlatformAdmin    platformadminhttp.Service
	PlatformTenant   platformtenanthttp.Service
	Provisioning     provisioninghttp.Service
	Settings         settingshttp.Service
	PlatformSettings settingshttp.PlatformService
	Audit            audithttp.Service
	PlatformAudit    audithttp.PlatformReader
	Ready            func(context.Context) error
}

// Places is the published global catalogue API consumed by HTTP delivery.
type Places = placeshttp.Service

// PlatformPlaces is the private catalogue management capability.
type PlatformPlaces = placeshttp.PlatformService

// Identity is the tenant identity API consumed by HTTP delivery.
type Identity = identityhttp.Service

// Authorizer is the permission API consumed by organization HTTP delivery.
type Authorizer = organizationhttp.Authorizer

// Authorization is the tenant role and policy API consumed by HTTP delivery.
type Authorization = authorizationhttp.Service

// Organization is the organization API consumed by HTTP delivery.
type Organization = organizationhttp.Service

// PlatformAdmin is the global administrator API consumed by HTTP delivery.
type PlatformAdmin = platformadminhttp.Service

// PlatformTenant is the tenant lifecycle API consumed by HTTP delivery.
type PlatformTenant = platformtenanthttp.Service

// Provisioning is the tenant provisioning API consumed by HTTP delivery.
type Provisioning = provisioninghttp.Service

// Settings is the tenant settings API consumed by HTTP delivery.
type Settings = settingshttp.Service

// PlatformSettings is the global settings API consumed by HTTP delivery.
type PlatformSettings = settingshttp.PlatformService

// Audit is the tenant audit query API consumed by HTTP delivery.
type Audit = audithttp.Service

// PlatformAudit is the global audit query API consumed by HTTP delivery.
type PlatformAudit = audithttp.PlatformReader

// Handler contains no business behavior; embedding composes the operation sets.
type Handler struct {
	*placeshttp.PlatformPlacesHandler
	*placeshttp.PlacesHandler
	*identityhttp.IdentityHandler
	*authorizationhttp.AuthorizationHandler
	*organizationhttp.OrganizationHandler
	*platformadminhttp.PlatformAdminHandler
	*platformtenanthttp.PlatformTenantHandler
	*provisioninghttp.ProvisioningHandler
	*settingshttp.SettingsHandler
	*settingshttp.PlatformSettingsHandler
	*audithttp.AuditHandler
	*audithttp.PlatformAuditHandler
	*SystemHandler
}

// New composes module-owned adapters into the complete generated server interface.
func New(deps Dependencies, cookieSecure bool, newCSRF func() (string, error)) *Handler {
	security := apihttp.NewSecurity(cookieSecure, newCSRF)
	identityHandler := identityhttp.NewHandler(deps.Identity, deps.Authorizer, security)
	platformAdminHandler := platformadminhttp.NewHandler(deps.PlatformAdmin, security)
	handler := &Handler{
		PlatformPlacesHandler:   placeshttp.NewPlatformHandler(deps.PlatformPlaces, platformAdminHandler, security),
		PlacesHandler:           placeshttp.NewHandler(deps.Places, security),
		IdentityHandler:         identityHandler,
		AuthorizationHandler:    authorizationhttp.NewHandler(deps.Authorization, identityHandler, security),
		OrganizationHandler:     organizationhttp.NewHandler(deps.Organization, deps.Authorizer, identityHandler, security),
		PlatformAdminHandler:    platformAdminHandler,
		PlatformTenantHandler:   platformtenanthttp.NewHandler(deps.PlatformTenant, platformAdminHandler, security),
		ProvisioningHandler:     provisioninghttp.NewHandler(deps.Provisioning, platformAdminHandler, security),
		SettingsHandler:         settingshttp.NewHandler(deps.Settings, deps.Authorizer, identityHandler, security),
		PlatformSettingsHandler: settingshttp.NewPlatformHandler(deps.PlatformSettings, platformAdminHandler, security),
		AuditHandler:            audithttp.NewHandler(deps.Audit, deps.Authorizer, identityHandler, security),
		SystemHandler:           newSystemHandler(deps.Ready, security),
	}
	handler.PlatformAuditHandler = audithttp.NewPlatformHandler(deps.PlatformAudit, platformAdminHandler, security)
	return handler
}

var _ generated.ServerInterface = (*Handler)(nil)
