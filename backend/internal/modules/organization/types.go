// Package organization owns departments, positions, and user organization assignments.
package organization

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

var (
	// ErrNotFound hides whether an organization resource exists in another tenant.
	ErrNotFound = errors.New("organization resource not found")
	// ErrCycle means a department move would create a cycle.
	ErrCycle = errors.New("department cycle")
	// ErrRootDepartment means an operation would violate the single-root invariant.
	ErrRootDepartment = errors.New("root department invariant")
	// ErrInUse means dependent organization records prevent deletion.
	ErrInUse = errors.New("organization resource is in use")
)

// PositionStatus is the lifecycle state of a tenant position.
type PositionStatus string

const (
	// PositionStatusActive permits the position to be assigned.
	PositionStatusActive PositionStatus = "active"
	// PositionStatusDisabled retains the position but prevents new use.
	PositionStatusDisabled PositionStatus = "disabled"
)

func (s PositionStatus) valid() bool {
	return s == PositionStatusActive || s == PositionStatusDisabled
}

// DepartmentID identifies a tenant-owned department.
type DepartmentID string

// PositionID identifies a tenant-owned position.
type PositionID string

// Department is the state needed to create a department.
type Department struct {
	ID             DepartmentID
	TenantID       identity.TenantID
	ParentID       *DepartmentID
	Name           string
	NormalizedName string
	SortOrder      int
	CreatedAt      time.Time
}

// DepartmentView is the stable application projection returned to transports.
type DepartmentView struct {
	ID        DepartmentID
	ParentID  *DepartmentID
	Name      string
	SortOrder int
}

// Position is the state needed to create a position.
type Position struct {
	ID             PositionID
	TenantID       identity.TenantID
	Name           string
	NormalizedName string
	CreatedAt      time.Time
}

// PositionView is the stable application projection returned to transports.
type PositionView struct {
	ID     PositionID
	Name   string
	Status PositionStatus
}

// NormalizeName canonicalizes an organization business name.
func NormalizeName(value string) string {
	return strings.Map(unicode.ToLower, strings.TrimSpace(value))
}
