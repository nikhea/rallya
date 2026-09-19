package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/order/model"
)

// OrderRepository is GORM persistence for the Orders domain.
type OrderRepository struct {
	db *gorm.DB
}

// NewOrderRepository wraps db for order persistence.
func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// DB exposes the handle for service-level transactions.
func (r *OrderRepository) DB() *gorm.DB { return r.db }

// CreateOrder inserts an order.
func (r *OrderRepository) CreateOrder(db *gorm.DB, o *model.Order) error {
	return dbOr(r, db).Create(o).Error
}

// GetOrderByID loads an order.
func (r *OrderRepository) GetOrderByID(id uuid.UUID) (*model.Order, error) {
	var o model.Order
	if err := r.db.First(&o, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrderByIdempotency finds a user's order by idempotency key.
func (r *OrderRepository) GetOrderByIdempotency(userID uuid.UUID, key string) (*model.Order, error) {
	var o model.Order
	if err := r.db.First(&o, "user_id = ? AND idempotency_key = ?", userID, key).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// ListUserOrders returns a user's orders, newest first.
func (r *OrderRepository) ListUserOrders(userID uuid.UUID, limit, offset int) ([]model.Order, int64, error) {
	var out []model.Order
	var total int64
	q := r.db.Model(&model.Order{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UpdateOrder saves order changes.
func (r *OrderRepository) UpdateOrder(db *gorm.DB, o *model.Order) error {
	return dbOr(r, db).Save(o).Error
}

// ExpiredPending lists held orders past their deadline (PENDING and
// PENDING_PAYMENT alike).
func (r *OrderRepository) ExpiredPending(now time.Time, limit int) ([]model.Order, error) {
	var out []model.Order
	if err := r.db.Where("status IN ? AND expires_at IS NOT NULL AND expires_at <= ?",
		[]model.OrderStatus{model.OrderStatusPending, model.OrderStatusPendingPayment}, now).Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func dbOr(r *OrderRepository, db *gorm.DB) *gorm.DB {
	if db != nil {
		return db
	}
	return r.db
}
