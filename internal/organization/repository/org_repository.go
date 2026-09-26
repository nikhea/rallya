package repository

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/organization/model"
)

// OrgRepository is GORM persistence for the Organization domain.
// No business logic; guards and role rules live in service.
type OrgRepository struct {
	db *gorm.DB
}

// NewOrgRepository wraps db for org persistence.
func NewOrgRepository(db *gorm.DB) *OrgRepository {
	return &OrgRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *OrgRepository) DB() *gorm.DB { return r.db }

// ---------- organizations ----------

// CreateOrg inserts an organization.
func (r *OrgRepository) CreateOrg(db *gorm.DB, o *model.Organization) error {
	return dbOr(r, db).Create(o).Error
}

// GetOrgByID loads an org by UUID.
func (r *OrgRepository) GetOrgByID(id uuid.UUID) (*model.Organization, error) {
	var o model.Organization
	if err := r.db.First(&o, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrgBySlug loads an org by slug.
func (r *OrgRepository) GetOrgBySlug(slug string) (*model.Organization, error) {
	var o model.Organization
	if err := r.db.First(&o, "slug = ?", slug).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// ListOrgs returns orgs newest-first with name/slug search (platform reads;
// hard deletes never list — GORM scopes them out by default).
func (r *OrgRepository) ListOrgs(query string, limit, offset int) ([]model.Organization, int64, error) {
	var out []model.Organization
	var total int64
	q := r.db.Model(&model.Organization{})
	if query != "" {
		like := "%" + strings.ToLower(query) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(slug) LIKE ?", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateOrg saves name/slug/logo changes.
func (r *OrgRepository) UpdateOrg(db *gorm.DB, o *model.Organization) error {
	return dbOr(r, db).Save(o).Error
}

// DeleteOrg hard-deletes an org (memberships/invites cascade in DDL).
func (r *OrgRepository) DeleteOrg(db *gorm.DB, id uuid.UUID) error {
	return dbOr(r, db).Delete(&model.Organization{}, "id = ?", id).Error
}

// ---------- memberships ----------

// CreateMembership inserts a membership row.
func (r *OrgRepository) CreateMembership(db *gorm.DB, m *model.Membership) error {
	return dbOr(r, db).Create(m).Error
}

// GetMembership loads one membership.
func (r *OrgRepository) GetMembership(orgID, userID uuid.UUID) (*model.Membership, error) {
	var m model.Membership
	if err := r.db.First(&m, "organization_id = ? AND user_id = ?", orgID, userID).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// CountMembers returns the membership headcount of an org.
func (r *OrgRepository) CountMembers(orgID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.Model(&model.Membership{}).Where("organization_id = ?", orgID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// ListMemberships returns all memberships of an org.
func (r *OrgRepository) ListMemberships(orgID uuid.UUID, limit, offset int) ([]model.Membership, int64, error) {
	var out []model.Membership
	var total int64
	q := r.db.Model(&model.Membership{}).Where("organization_id = ?", orgID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at ASC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListUserMemberships returns all memberships of a user with orgs preloaded.
func (r *OrgRepository) ListUserMemberships(userID uuid.UUID) ([]model.Membership, error) {
	var out []model.Membership
	if err := r.db.Order("created_at ASC").Find(&out, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateMemberRole sets a membership role.
func (r *OrgRepository) UpdateMemberRole(db *gorm.DB, orgID, userID uuid.UUID, role model.MemberRole, at time.Time) error {
	return dbOr(r, db).Model(&model.Membership{}).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Updates(map[string]any{"role": role, "updated_at": at}).Error
}

// SetNotifyEvents updates the announcement preference.
func (r *OrgRepository) SetNotifyEvents(db *gorm.DB, orgID, userID uuid.UUID, notify bool, at time.Time) error {
	return dbOr(r, db).Model(&model.Membership{}).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Updates(map[string]any{"notify_events": notify, "updated_at": at}).Error
}

// DeleteMembership removes one membership.
func (r *OrgRepository) DeleteMembership(db *gorm.DB, orgID, userID uuid.UUID) error {
	return dbOr(r, db).Delete(&model.Membership{},
		"organization_id = ? AND user_id = ?", orgID, userID).Error
}

// CountOwners counts OWNER memberships in an org.
func (r *OrgRepository) CountOwners(orgID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.Model(&model.Membership{}).
		Where("organization_id = ? AND role = ?", orgID, model.MemberRoleOwner).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// ---------- invites ----------

// CreateInvite inserts an invite row.
func (r *OrgRepository) CreateInvite(db *gorm.DB, in *model.Invite) error {
	return dbOr(r, db).Create(in).Error
}

// GetInviteByID loads an invite by ID.
func (r *OrgRepository) GetInviteByID(id uuid.UUID) (*model.Invite, error) {
	var in model.Invite
	if err := r.db.First(&in, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

// GetInviteByHash loads an invite by token hash.
func (r *OrgRepository) GetInviteByHash(hash string) (*model.Invite, error) {
	var in model.Invite
	if err := r.db.First(&in, "token_hash = ?", hash).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

// PendingInvite finds an unconsumed invite for org+email, if any.
// Takes db because callers invoke it inside transactions (read-your-write:
// on pooled connections — and especially :memory: SQLite — r.db may see
// a different snapshot than the open tx).
func (r *OrgRepository) PendingInvite(db *gorm.DB, orgID uuid.UUID, email string) (*model.Invite, error) {
	var in model.Invite
	if err := dbOr(r, db).
		Where("organization_id = ? AND email = ? AND accepted_at IS NULL AND declined_at IS NULL", orgID, email).
		Order("created_at DESC").First(&in).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

// ListInvites returns pending invites of an org.
func (r *OrgRepository) ListInvites(orgID uuid.UUID, limit, offset int) ([]model.Invite, int64, error) {
	var out []model.Invite
	var total int64
	q := r.db.Model(&model.Invite{}).
		Where("organization_id = ? AND accepted_at IS NULL AND declined_at IS NULL", orgID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ConsumeInvite stamps accepted_at or declined_at.
func (r *OrgRepository) ConsumeInvite(db *gorm.DB, id uuid.UUID, accepted bool, at time.Time) error {
	field := "declined_at"
	if accepted {
		field = "accepted_at"
	}
	return dbOr(r, db).Model(&model.Invite{}).
		Where("id = ?", id).
		Update(field, at).Error
}

// ---------- custom roles ----------

// CreateRoleDef inserts a role definition.
func (r *OrgRepository) CreateRoleDef(db *gorm.DB, d *model.RoleDefinition) error {
	return dbOr(r, db).Create(d).Error
}

// GetRoleDef loads one definition by org + lowercase name.
func (r *OrgRepository) GetRoleDef(orgID uuid.UUID, name string) (*model.RoleDefinition, error) {
	var d model.RoleDefinition
	if err := r.db.First(&d, "org_id = ? AND name = ?", orgID, name).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// ListRoleDefs returns an org's definitions, oldest first.
func (r *OrgRepository) ListRoleDefs(orgID uuid.UUID) ([]model.RoleDefinition, error) {
	var out []model.RoleDefinition
	if err := r.db.Where("org_id = ?", orgID).Order("created_at ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateRoleDef saves definition changes.
func (r *OrgRepository) UpdateRoleDef(db *gorm.DB, d *model.RoleDefinition) error {
	return dbOr(r, db).Save(d).Error
}

// DeleteRoleDef removes a definition (callers enforce unassigned-first).
func (r *OrgRepository) DeleteRoleDef(db *gorm.DB, orgID uuid.UUID, name string) error {
	return dbOr(r, db).Delete(&model.RoleDefinition{}, "org_id = ? AND name = ?", orgID, name).Error
}

// CountRoleDefs tallies an org's definitions (20-per-org cap).
func (r *OrgRepository) CountRoleDefs(orgID uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.Model(&model.RoleDefinition{}).Where("org_id = ?", orgID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// AssignCustomRole grants one custom role (unique violation = already held;
// callers map it to idempotent success).
func (r *OrgRepository) AssignCustomRole(db *gorm.DB, orgID, userID uuid.UUID, role string) error {
	return dbOr(r, db).Create(&model.MemberCustomRole{
		OrganizationID: orgID, UserID: userID, RoleName: role,
	}).Error
}

// UnassignCustomRole revokes one custom role (idempotent: missing rows are
// a no-op success).
func (r *OrgRepository) UnassignCustomRole(db *gorm.DB, orgID, userID uuid.UUID, role string) error {
	return dbOr(r, db).Delete(&model.MemberCustomRole{},
		"org_id = ? AND user_id = ? AND role_name = ?", orgID, userID, role).Error
}

// ListUserCustomRoles returns a member's custom role names.
// Takes db because removal reads inside the caller's tx (read-your-write;
// a shared-handle read would deadlock single-conn test DBs).
func (r *OrgRepository) ListUserCustomRoles(db *gorm.DB, orgID, userID uuid.UUID) ([]string, error) {
	var rows []model.MemberCustomRole
	if err := dbOr(r, db).Where("org_id = ? AND user_id = ?", orgID, userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.RoleName)
	}
	return out, nil
}

// CountRoleAssignments tallies holders of one role (delete guard).
func (r *OrgRepository) CountRoleAssignments(orgID uuid.UUID, role string) (int64, error) {
	var n int64
	if err := r.db.Model(&model.MemberCustomRole{}).
		Where("org_id = ? AND role_name = ?", orgID, role).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountRoleAssignees tallies holders per role for list views.
func (r *OrgRepository) CountRoleAssignees(orgID uuid.UUID) (map[string]int64, error) {
	type row struct {
		RoleName string
		N        int64
	}
	var rows []row
	if err := r.db.Model(&model.MemberCustomRole{}).
		Select("role_name, COUNT(*) AS n").
		Where("org_id = ?", orgID).
		Group("role_name").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, row := range rows {
		out[row.RoleName] = row.N
	}
	return out, nil
}

func dbOr(r *OrgRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
