package service

import (
	"context"
	"fmt"
	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/tenancy"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/pkg/hash"
)

// TrialLength is how long a new company may use the product before choosing
// a plan.
const TrialLength = model.TrialDays * 24 * time.Hour

// TenantService manages companies: sign-up, settings, usage, and the
// platform operator's view across them.
type TenantService struct {
	*core
	auth    *AuthService
	billing *BillingService
}

// SignupInput is the self-service registration form.
type SignupInput struct {
	Company   string
	City      string
	AdminName string
	Email     string
	Phone     string
	Password  string
	// Plan is the plan to pay for straight away, or empty (or trial) to
	// start with the free trial; Cycle its billing cycle.
	Plan  string
	Cycle string
}

// A sign-up waits this long for the code texted to the admin's phone.
const signupTTL = time.Hour

// newCompany is a validated sign-up.
type newCompany struct {
	name, city   string
	admin        validPerson
	passwordHash string
	plan, cycle  string
}

func (in SignupInput) validate() (newCompany, error) {
	var c newCompany
	c.name = strings.Join(strings.Fields(in.Company), " ")
	if utf8.RuneCountInString(c.name) < 2 {
		return c, invalid("company", "Enter your company's name.")
	}
	if utf8.RuneCountInString(c.name) > 120 {
		return c, invalid("company", "That name is too long.")
	}
	c.city = "Addis Ababa"
	if strings.TrimSpace(in.City) != "" {
		p, ok := geo.Lookup(in.City)
		if !ok {
			return c, invalid("city", "Pick your base town from the list.")
		}
		c.city = p.Name
	}
	var err error
	c.admin, err = validatePerson(PersonInput{FullName: in.AdminName, Phone: in.Phone, Email: in.Email}, true)
	if err != nil {
		return c, err
	}
	if c.admin.email == nil {
		return c, invalid("email", "Enter your email address.")
	}
	if err := checkPassword(in.Password); err != nil {
		e := *err.(*Error) // a copy: checkPassword may return a shared value
		e.Field = "password"
		return c, &e
	}
	c.plan, c.cycle = in.Plan, in.Cycle
	if c.plan == "" {
		c.plan = model.PlanTrial
	}
	if c.cycle == "" {
		c.cycle = model.CycleMonthly
	}
	if p, ok := model.PlanByCode(c.plan); !ok || !(p.SelfServe || p.Code == model.PlanTrial) {
		return c, invalid("plan", "Choose the free trial or a plan.")
	}
	if !model.ValidCycle(c.cycle) {
		return c, invalid("cycle", "Choose monthly or annual billing.")
	}
	c.passwordHash, err = hash.Password(in.Password)
	return c, err
}

// StartSignup checks a registration and texts a code to the admin's phone.
// The company is created only once the code comes back (CompleteSignup): its
// phone is how drivers, customers and the platform reach the company, so it
// must be one the person holds. The token identifies the waiting sign-up to
// the browser that started it.
func (s *TenantService) StartSignup(ctx context.Context, in SignupInput) (token string, err error) {
	c, err := in.validate()
	if err != nil {
		return "", err
	}
	// Whether the email or phone is already registered is told only once
	// the code comes back (CompleteSignup), to someone who holds the phone:
	// telling it here would let anyone check who has an account.
	if err := s.issueCode(ctx, pgtype.UUID{}, *c.admin.phone, codePurposeSignup, s.signupText); err != nil {
		return "", err
	}
	return s.signups.Hold(ctx, identity.NewSignup{
		Company: c.name, City: c.city, FullName: c.admin.name,
		Email: *c.admin.email, Phone: *c.admin.phone, PasswordHash: c.passwordHash,
		Plan: c.plan, BillingCycle: c.cycle, Expires: s.now().Add(signupTTL),
	})
}

func (s *TenantService) signupText(code string) string {
	return fmt.Sprintf("%s: your sign-up code is %s. It expires in 10 minutes. Never share it.", s.cfg.AppName, code)
}

// PendingSignup is a sign-up waiting for its code.
func (s *TenantService) PendingSignup(ctx context.Context, token string) (identity.PendingSignup, error) {
	return s.signups.Find(ctx, s.q.Conn(), token)
}

// ResendSignupCode texts a new code for a waiting sign-up.
func (s *TenantService) ResendSignupCode(ctx context.Context, token string) error {
	ps, err := s.PendingSignup(ctx, token)
	if err != nil {
		return err
	}
	return s.issueCode(ctx, pgtype.UUID{}, ps.Phone, codePurposeSignup, s.signupText)
}

// SignupResult is a company just created, with its admin signed in.
type SignupResult struct {
	*LoginResult
	// Invoice is the first period's invoice, when the company chose to pay
	// for a plan straight away.
	Invoice *db.Invoice
}

// CompleteSignup checks the texted code, creates the company on a free trial
// with its first admin, emails them a link to confirm their address, and
// signs them in. A company that chose a plan at sign-up gets its first
// invoice too; the plan starts when the trial ends or, if paid later, on
// payment.
func (s *TenantService) CompleteSignup(ctx context.Context, token, code string, ci ClientInfo) (*SignupResult, error) {
	var admin db.User
	var plan, cycle string
	err := s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		// Locked, so a second submission of the same code waits for this
		// one and then finds the sign-up finished.
		ps, err := s.signups.Find(ctx, q.Conn(), token)
		if err != nil {
			return err
		}
		// The try is counted whatever happens next, but the code is used
		// up only with the company: if creating it fails, the same code
		// can be entered again.
		codeID, err := s.verifyCode(ctx, ps.Phone, codePurposeSignup, strings.TrimSpace(code))
		if err != nil {
			return err
		}
		if err := s.consumeCode(ctx, q, codeID); err != nil {
			return err
		}
		if err := s.signupClash(ctx, q, ps.Email, ps.Phone); err != nil {
			return err
		}
		admin, err = s.createCompany(ctx, q, newCompany{
			name: ps.Company, city: ps.City, passwordHash: ps.PasswordHash,
			admin: validPerson{name: ps.FullName, phone: &ps.Phone, email: &ps.Email},
		}, ci)
		if err != nil {
			return err
		}
		if err := s.sendEmailVerification(ctx, q, after, admin,
			fmt.Sprintf("Welcome to %s. %s is set up, with a free trial for the next %d days.", s.cfg.AppName, ps.Company, model.TrialDays)); err != nil {
			return err
		}
		plan, cycle = ps.Plan, ps.BillingCycle
		return s.signups.Done(ctx, q.Conn(), ps.ID)
	})
	if err != nil {
		return nil, err
	}
	login, err := s.auth.startSession(ctx, admin, SessionWeb, false, ci)
	if err != nil {
		return nil, err
	}
	res := &SignupResult{LoginResult: login}
	if plan != "" && plan != model.PlanTrial {
		// The company exists whatever happens here: a failure leaves it on
		// the trial, from which the plan can be chosen again.
		change, err := s.billing.ChoosePlan(ctx, login.Principal, plan, cycle, ci)
		if err != nil {
			s.log.Error().Err(err).Str("plan", plan).Msg("plan chosen at sign-up")
		} else {
			res.Invoice = change.Invoice
		}
	}
	return res, nil
}

// CreateCompany registers a company whose admin's phone and email have been
// checked some other way: the demo data and the platform operator's own
// account. Self-service sign-up goes through StartSignup and CompleteSignup.
func (s *TenantService) CreateCompany(ctx context.Context, in SignupInput, ci ClientInfo) (*LoginResult, error) {
	c, err := in.validate()
	if err != nil {
		return nil, err
	}
	var admin db.User
	err = s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		if admin, err = s.createCompany(ctx, q, c, ci); err != nil {
			return err
		}
		_, err = s.users.VerifyEmail(ctx, q.Conn(), admin.TenantID, admin.ID, deref(admin.Email))
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.auth.startSession(ctx, admin, SessionWeb, false, ci)
}

// signupClash refuses a sign-up whose email or phone already has an
// account. Told only after the code is checked, to the holder of the phone,
// on a page without those fields: so the error names no field.
func (c *core) signupClash(ctx context.Context, q *db.Queries, email, phone string) error {
	if _, err := c.userByEmail(ctx, q, email); err == nil {
		return &Error{Kind: KindInvalid, Message: "Someone is already registered with this email address. Sign in, or reset your password from the sign-in page. " +
			"To register another company, start again with a different address."}
	} else if !isNoRows(err) {
		return err
	}
	if _, err := c.userByPhone(ctx, q, &phone); err == nil {
		return &Error{Kind: KindInvalid, Message: "Someone is already registered with this phone number. Sign in, or start again with a different number."}
	} else if !isNoRows(err) {
		return err
	}
	return nil
}

// createCompany creates a company on a free trial and its first admin.
func (s *TenantService) createCompany(ctx context.Context, q *db.Queries, c newCompany, ci ClientInfo) (db.User, error) {
	trial, _ := model.PlanByCode(model.PlanTrial)
	created, err := s.tenancy.Create(ctx, q.Conn(), tenancy.NewCompany{
		Name: c.name, Plan: model.PlanTrial, MaxDrivers: trial.MaxDrivers,
		ContactPhone: deref(c.admin.phone), ContactEmail: deref(c.admin.email), City: c.city,
		TrialEndsAt: s.now().Add(TrialLength),
	})
	if err != nil {
		return db.User{}, err
	}
	tenant := db.Tenant(created)
	admin, err := s.createUser(ctx, q, identity.NewUser{
		TenantID: tenant.ID, Role: model.RoleAdmin, FullName: c.admin.name,
		Phone: c.admin.phone, Email: c.admin.email, PasswordHash: &c.passwordHash,
	})
	if err != nil {
		return admin, s.duplicateUser(ctx, tenant.ID, c.admin, err)
	}
	s.audit(ctx, q, userActor(admin, ci), "company_signup", "tenant", tenant.ID, nil, map[string]any{"name": c.name})
	return admin, nil
}

// Get loads a tenant.
func (s *TenantService) Get(ctx context.Context, id pgtype.UUID) (db.Tenant, error) {
	return s.tenant(ctx, s.q, id)
}

// SettingsInput is the company settings form (tenancy.Settings).
type SettingsInput = tenancy.Settings

// UpdateSettings saves the company profile and notification preferences.
func (s *TenantService) UpdateSettings(ctx context.Context, p *Principal, in SettingsInput, ci ClientInfo) (db.Tenant, error) {
	if !p.CanManageCompany() {
		return db.Tenant{}, ErrUnauthorized
	}
	cur, t, err := s.tenancy.UpdateSettings(ctx, s.q.Conn(), p.TenantID, in)
	if err != nil {
		return db.Tenant(t), err
	}
	s.audit(ctx, s.q, p.Actor(SourceDashboard, ci.IP, ci.UserAgent), "settings_updated", "tenant", t.ID, cur, t)
	return db.Tenant(t), nil
}

// Usage is a company's consumption against its plan.
type Usage struct {
	Plan               model.Plan
	ActiveDrivers      int64
	MaxDrivers         int32
	Vehicles           int64
	Staff              int64
	ShipmentsThisMonth int64
	DeliveredThisMonth int64
	// SMSThisMonth counts the texts sent this calendar month that count
	// against SMSIncluded, the month's allowance: on a plan that started
	// this month, those since it started.
	SMSThisMonth int64
	SMSIncluded  int32
	TrialEnds    *time.Time
}

// Usage reports a company's consumption this calendar month.
func (s *TenantService) Usage(ctx context.Context, tenantID pgtype.UUID) (*Usage, error) {
	t, err := s.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	now := s.now().In(s.cfg.Location)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, s.cfg.Location)
	u := &Usage{MaxDrivers: t.MaxDrivers, SMSIncluded: t.SmsMonthQuota, TrialEnds: timePtr(t.TrialEndsAt)}
	u.Plan, _ = model.PlanByCode(t.Plan)
	if u.Plan.Code == "" {
		u.Plan = model.Plan{Code: t.Plan, Name: t.Plan, MaxDrivers: t.MaxDrivers}
	}
	if u.ActiveDrivers, err = s.users.CountDrivers(ctx, s.q.Conn(), tenantID); err != nil {
		return nil, err
	}
	if u.Vehicles, err = s.fleet.CountVehicles(ctx, s.q.Conn(), tenantID); err != nil {
		return nil, err
	}
	if u.Staff, err = s.users.CountStaff(ctx, s.q.Conn(), tenantID); err != nil {
		return nil, err
	}
	if u.ShipmentsThisMonth, err = s.q.CountShipmentsCreatedSince(ctx, db.CountShipmentsCreatedSinceParams{TenantID: tenantID, CreatedAt: toTimestamp(month)}); err != nil {
		return nil, err
	}
	if u.DeliveredThisMonth, err = s.q.CountDeliveredSince(ctx, db.CountDeliveredSinceParams{TenantID: tenantID, ActualDelivery: toTimestamp(month)}); err != nil {
		return nil, err
	}
	// A trial's allowance covers the whole trial (smsRefusal), so its
	// meter counts from the start.
	since := month
	if t.Plan == model.PlanTrial {
		since = t.CreatedAt.Time
	} else if t.SubscribedAt.Valid && t.SubscribedAt.Time.After(since) {
		since = t.SubscribedAt.Time
	}
	if u.SMSThisMonth, err = s.q.CountSMSSentSince(ctx, db.CountSMSSentSinceParams{TenantID: tenantID, SentAt: toTimestamp(since)}); err != nil {
		return nil, err
	}
	return u, nil
}

// All lists every company with activity figures, for the platform operator.
func (s *TenantService) All(ctx context.Context, p *Principal) ([]db.ListTenantsWithStatsRow, error) {
	if !p.IsPlatformAdmin() {
		return nil, ErrUnauthorized
	}
	return s.q.ListTenantsWithStats(ctx)
}

// SetActive suspends or restores a company. Suspension signs everyone in it
// out on their next request.
func (s *TenantService) SetActive(ctx context.Context, p *Principal, id pgtype.UUID, active bool, ci ClientInfo) error {
	if !p.IsPlatformAdmin() {
		return ErrUnauthorized
	}
	if id == p.TenantID {
		return invalid("", "You can't suspend the platform's own account.")
	}
	if err := s.tenancy.SetActive(ctx, s.q.Conn(), id, active); err != nil {
		return err
	}
	action := "tenant_suspended"
	if active {
		action = "tenant_restored"
	}
	s.audit(ctx, s.q, p.Actor(SourceDashboard, ci.IP, ci.UserAgent), action, "tenant", id, nil, nil)
	return nil
}
