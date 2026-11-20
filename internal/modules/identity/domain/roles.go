// Package domain is the identity module's rules, without I/O.
package domain

import "github.com/kiflet/backend/internal/platform/text"

// Roles.
const (
	RoleDriver        = "driver"
	RoleDispatcher    = "dispatcher"
	RoleFleetManager  = "fleet_manager"
	RoleAdmin         = "admin"
	RolePlatformAdmin = "platform_admin"
)

// StaffRoles are the roles an admin can give a dashboard user.
var StaffRoles = []string{RoleDispatcher, RoleFleetManager, RoleAdmin}

// RoleLabel is a role as people read it.
func RoleLabel(r string) string {
	return text.Label(text.English, text.Office, text.KindRole, r)
}

// CanManageFleet reports whether a role may manage drivers and vehicles
// (FR-004 AC-004.3).
func CanManageFleet(role string) bool {
	return role == RoleFleetManager || role == RoleAdmin || role == RolePlatformAdmin
}

// CanManageCompany reports whether a role may manage staff accounts and
// company settings (FR-004 AC-004.4).
func CanManageCompany(role string) bool {
	return role == RoleAdmin || role == RolePlatformAdmin
}
