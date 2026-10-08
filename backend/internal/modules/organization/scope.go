package organization

import "github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"

// DataScope is the SQL-free visibility input accepted by organization stores.
type DataScope struct {
	ActorID                  identity.UserID
	All                      bool
	Self                     bool
	Department               bool
	DepartmentAndDescendants bool
	CustomDepartmentIDs      []DepartmentID
}

func (s DataScope) valid() bool {
	return s.ActorID != "" && (s.All || s.Self || s.Department || s.DepartmentAndDescendants || len(s.CustomDepartmentIDs) > 0)
}
