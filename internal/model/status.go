package model

import (
	"github.com/kiflet/backend/internal/modules/identity/domain"
	shipments "github.com/kiflet/backend/internal/modules/shipments/domain"
)

// The shipment statuses, their transitions, reasons and priorities are
// the shipments module's (internal/modules/shipments/domain).
type (
	Actor           = shipments.Actor
	TransitionError = shipments.TransitionError
)

const (
	StatusPending   = shipments.StatusPending
	StatusAssigned  = shipments.StatusAssigned
	StatusInTransit = shipments.StatusInTransit
	StatusDelayed   = shipments.StatusDelayed
	StatusArrived   = shipments.StatusArrived
	StatusDelivered = shipments.StatusDelivered
	StatusIssue     = shipments.StatusIssue
	StatusCancelled = shipments.StatusCancelled
	ActorDriver     = shipments.ActorDriver
	ActorDriverSMS  = shipments.ActorDriverSMS
	ActorDispatcher = shipments.ActorDispatcher
	ActorPOD        = shipments.ActorPOD
	ActorReview     = shipments.ActorReview
	PriorityNormal  = shipments.PriorityNormal
	PriorityHigh    = shipments.PriorityHigh
	PriorityUrgent  = shipments.PriorityUrgent
)

var (
	AllStatuses        = shipments.AllStatuses
	ActiveStatuses     = shipments.ActiveStatuses
	OnRoadStatuses     = shipments.OnRoadStatuses
	ErrSameStatus      = shipments.ErrSameStatus
	ErrPODRequired     = shipments.ErrPODRequired
	DelayReasons       = shipments.DelayReasons
	IssueReasons       = shipments.IssueReasons
	CancelReasons      = shipments.CancelReasons
	LocationReasons    = shipments.LocationReasons
	Priorities         = shipments.Priorities
	IsValidStatus      = shipments.IsValidStatus
	IsActiveStatus     = shipments.IsActiveStatus
	IsOnRoad           = shipments.IsOnRoad
	IsFinalStatus      = shipments.IsFinalStatus
	StatusLabel        = shipments.StatusLabel
	CheckTransition    = shipments.CheckTransition
	AllowedTransitions = shipments.AllowedTransitions
	CanCancel          = shipments.CanCancel
	CanReopen          = shipments.CanReopen
	CanAssign          = shipments.CanAssign
	CanUnassign        = shipments.CanUnassign
	CancelReasonsFor   = shipments.CancelReasonsFor
	ReasonLabel        = shipments.ReasonLabel
	ReasonsFor         = shipments.ReasonsFor
)

// Roles are the identity module's (internal/modules/identity/domain).
const (
	RoleDriver        = domain.RoleDriver
	RoleDispatcher    = domain.RoleDispatcher
	RoleFleetManager  = domain.RoleFleetManager
	RoleAdmin         = domain.RoleAdmin
	RolePlatformAdmin = domain.RolePlatformAdmin
)

var (
	// StaffRoles are the roles an admin can give a dashboard user.
	StaffRoles       = domain.StaffRoles
	RoleLabel        = domain.RoleLabel
	CanManageFleet   = domain.CanManageFleet
	CanManageCompany = domain.CanManageCompany
	// CompanyRoles are the roles CanManageCompany allows.
	CompanyRoles = domain.CompanyRoles
)
