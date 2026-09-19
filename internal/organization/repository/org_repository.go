package repository

import (
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

func dbOr(r *OrgRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
