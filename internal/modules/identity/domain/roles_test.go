package domain

import (
	"slices"
	"testing"
)

// CompanyRoles, who are sent the company's billing updates, are exactly
// the roles that may manage the company.
func TestCompanyRoles(t *testing.T) {
	for _, r := range []string{RoleDriver, RoleDispatcher, RoleFleetManager, RoleAdmin, RolePlatformAdmin} {
		if CanManageCompany(r) != slices.Contains(CompanyRoles, r) {
			t.Errorf("%s: CanManageCompany %v, in CompanyRoles %v", r, CanManageCompany(r), slices.Contains(CompanyRoles, r))
		}
	}
}
