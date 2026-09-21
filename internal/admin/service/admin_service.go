package service

import (
	"errors"
	"strings"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"

	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/iam"
	orderdto "github.com/nikhea/rallya/internal/order/dto"
	ordermodel "github.com/nikhea/rallya/internal/order/model"
	orderrepo "github.com/nikhea/rallya/internal/order/repository"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepo "github.com/nikhea/rallya/internal/organization/repository"
)

// AdminService serves platform reads over tenant rows. It owns no tables:
// repositories are composed read-only, and every call emits an audit entry
// (actor = superadmin) so snooping is answerable.
type AdminService struct {
	orgs    *orgrepo.OrgRepository
	users   *authrepo.AuthRepository
	orders  *orderrepo.OrderRepository
	enforce *casbin.Enforcer
	auditor auditsvc.Emitter
}

// NewAdminService builds the service. All collaborators required except
// the emitter (nil-safe when absent — tests).
func NewAdminService(orgs *orgrepo.OrgRepository, users *authrepo.AuthRepository, orders *orderrepo.OrderRepository, e *casbin.Enforcer) *AdminService {
	return &AdminService{orgs: orgs, users: users, orders: orders, enforce: e}
}

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *AdminService) SetAuditEmitter(a auditsvc.Emitter) { s.auditor = a }

// emit records a platform-read entry (nil-safe when unwired).
func (s *AdminService) emit(actorID uuid.UUID, orgID *uuid.UUID, action, objectType string, objectID *uuid.UUID) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.EmitTx(nil, auditsvc.Entry{
		ActorID: &actorID, OrgID: orgID,
		Action: action, ObjectType: objectType, ObjectID: objectID,
	})
}

// OrgRow is one inventory row with its member count.
type OrgRow struct {
	Org     orgmodel.Organization
	Members int64
}

// ListOrgs returns the platform org inventory, newest first.
func (s *AdminService) ListOrgs(actorID uuid.UUID, query string, limit, offset int) ([]OrgRow, int64, error) {
	orgs, total, err := s.orgs.ListOrgs(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]OrgRow, 0, len(orgs))
	for i := range orgs {
		_, members, err := s.orgs.ListMemberships(orgs[i].ID, 1, 0)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, OrgRow{Org: orgs[i], Members: members})
	}
	if err := s.emit(actorID, nil, "admin.orgs_listed", auditmodel.ObjectOrg, nil); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// OrgDetail is the dispute-triage view of one tenant.
type OrgDetail struct {
	Org     orgmodel.Organization
	Members []MemberView
}

// MemberView joins a membership row with identity.
type MemberView struct {
	User     authmodel.User
	Role     orgmodel.MemberRole
	JoinedAt time.Time
}

// GetOrg returns one tenant with its full roster.
func (s *AdminService) GetOrg(actorID, orgID uuid.UUID) (*OrgDetail, error) {
	o, err := s.orgs.GetOrgByID(orgID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	ms, _, err := s.orgs.ListMemberships(orgID, 10000, 0)
	if err != nil {
		return nil, err
	}
	members := make([]MemberView, 0, len(ms))
	for _, m := range ms {
		u, err := s.users.GetUserByID(m.UserID)
		if err != nil {
			continue // user deleted concurrently; skip
		}
		members = append(members, MemberView{User: *u, Role: m.Role, JoinedAt: m.CreatedAt})
	}
	if err := s.emit(actorID, &orgID, "admin.org_viewed", auditmodel.ObjectOrg, &orgID); err != nil {
		return nil, err
	}
	return &OrgDetail{Org: *o, Members: members}, nil
}

// UserRow is one account row with its superadmin flag.
type UserRow struct {
	User       authmodel.User
	SuperAdmin bool
}

// SearchUsers finds accounts by email fragment, newest first.
func (s *AdminService) SearchUsers(actorID uuid.UUID, query string, limit, offset int) ([]UserRow, int64, error) {
	users, total, err := s.users.SearchUsers(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]UserRow, 0, len(users))
	for i := range users {
		out = append(out, UserRow{User: users[i], SuperAdmin: iam.IsSuperAdmin(s.enforce, users[i].ID)})
	}
	if err := s.emit(actorID, nil, "admin.users_searched", auditmodel.ObjectUser, nil); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UserDetail is the access-review view of one account.
type UserDetail struct {
	User        authmodel.User
	SuperAdmin  bool
	Memberships []MembershipView
}

// MembershipView joins a membership with its org.
type MembershipView struct {
	Org      orgmodel.Organization
	Role     orgmodel.MemberRole
	JoinedAt time.Time
}

// GetUser returns one account with its tenant memberships.
func (s *AdminService) GetUser(actorID, userID uuid.UUID) (*UserDetail, error) {
	u, err := s.users.GetUserByID(userID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	ms, err := s.orgs.ListUserMemberships(userID)
	if err != nil {
		return nil, err
	}
	memberships := make([]MembershipView, 0, len(ms))
	for _, m := range ms {
		o, err := s.orgs.GetOrgByID(m.OrganizationID)
		if err != nil {
			continue // org deleted concurrently; skip
		}
		memberships = append(memberships, MembershipView{Org: *o, Role: m.Role, JoinedAt: m.CreatedAt})
	}
	if err := s.emit(actorID, nil, "admin.user_viewed", auditmodel.ObjectUser, &userID); err != nil {
		return nil, err
	}
	return &UserDetail{User: *u, SuperAdmin: iam.IsSuperAdmin(s.enforce, userID), Memberships: memberships}, nil
}

// ListUserOrders returns a user's cross-tenant order history (orderdto is
// already Stripe-free — no payment identifiers leak here).
func (s *AdminService) ListUserOrders(actorID, userID uuid.UUID, limit, offset int) ([]orderdto.Order, int64, error) {
	if _, err := s.users.GetUserByID(userID); err != nil {
		if isNotFound(err) {
			return nil, 0, ErrUserNotFound
		}
		return nil, 0, err
	}
	orders, total, err := s.orders.ListUserOrders(userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]orderdto.Order, 0, len(orders))
	for i := range orders {
		out = append(out, *toOrderDTO(&orders[i]))
	}
	if err := s.emit(actorID, nil, "admin.user_orders_viewed", auditmodel.ObjectUser, &userID); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func toOrderDTO(o *ordermodel.Order) *orderdto.Order {
	out := &orderdto.Order{
		ID: o.ID.String(), EventID: o.EventID.String(), TicketTypeID: o.TicketTypeID.String(),
		Quantity: o.Quantity, PriceCents: o.PriceCents, Currency: o.Currency,
		Status: string(o.Status), CreatedAt: o.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if o.ExpiresAt != nil {
		s := o.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
		out.ExpiresAt = &s
	}
	return out
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// PolicyDiff is the idempotent-repair report.
type PolicyDiff struct {
	Added   int
	Removed int
	Total   int
}

// ReseedOrgPolicies re-runs the org policy seed (adds missing rows; never
// removes) and reports the diff. Repairs drifted Casbin rows without DB
// surgery.
func (s *AdminService) ReseedOrgPolicies(actorID, orgID uuid.UUID) (*PolicyDiff, error) {
	if _, err := s.orgs.GetOrgByID(orgID); err != nil {
		if isNotFound(err) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	before, err := iam.CountOrgPolicies(s.enforce, orgID)
	if err != nil {
		return nil, err
	}
	if err := iam.SeedOrgPolicies(s.enforce, orgID); err != nil {
		return nil, err
	}
	after, err := iam.CountOrgPolicies(s.enforce, orgID)
	if err != nil {
		return nil, err
	}
	diff := &PolicyDiff{Added: after - before, Removed: 0, Total: after}
	if err := s.emit(actorID, &orgID, "admin.policies_reseeded", auditmodel.ObjectPolicy, &orgID); err != nil {
		return nil, err
	}
	return diff, nil
}

// SyncUserPolicies converges a user's groupings to membership truth:
// every membership gets its grouping (sweep-then-add), and groupings for
// orgs with no membership are swept. Superadmin g2 rows are untouched.
func (s *AdminService) SyncUserPolicies(actorID, userID uuid.UUID) (*PolicyDiff, error) {
	if _, err := s.users.GetUserByID(userID); err != nil {
		if isNotFound(err) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	before, err := groupingSet(s.enforce, userID)
	if err != nil {
		return nil, err
	}
	ms, err := s.orgs.ListUserMemberships(userID)
	if err != nil {
		return nil, err
	}
	syncer := iam.NewMembershipSyncer(s.enforce)
	keep := map[string]bool{}
	for _, m := range ms {
		if err := syncer.SyncMembership(userID, m.OrganizationID, &m.Role); err != nil {
			return nil, err
		}
		keep[m.OrganizationID.String()] = true
	}
	for orgRef := range beforeOrgs(before) {
		if !keep[orgRef] {
			orgID, err := uuid.Parse(orgRef)
			if err != nil {
				continue
			}
			if err := syncer.SyncMembership(userID, orgID, nil); err != nil {
				return nil, err
			}
		}
	}
	after, err := groupingSet(s.enforce, userID)
	if err != nil {
		return nil, err
	}
	added, removed := 0, 0
	for k := range after {
		if !before[k] {
			added++
		}
	}
	for k := range before {
		if !after[k] {
			removed++
		}
	}
	diff := &PolicyDiff{Added: added, Removed: removed, Total: len(after)}
	if err := s.emit(actorID, nil, "admin.policies_synced", auditmodel.ObjectUser, &userID); err != nil {
		return nil, err
	}
	return diff, nil
}

// groupingSet snapshots a user's 3-field groupings as role@org strings
// (g2 superadmin rows excluded — never membership state).
func groupingSet(e *casbin.Enforcer, userID uuid.UUID) (map[string]bool, error) {
	rows, err := iam.UserGroupings(e, userID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, row := range rows {
		if len(row) == 3 {
			out[row[1]+"@"+row[2]] = true
		}
	}
	return out, nil
}

// beforeOrgs lists the org domains in a grouping set.
func beforeOrgs(set map[string]bool) map[string]bool {
	orgs := map[string]bool{}
	for k := range set {
		if i := strings.Index(k, "@"); i >= 0 {
			orgs[k[i+1:]] = true
		}
	}
	return orgs
}
