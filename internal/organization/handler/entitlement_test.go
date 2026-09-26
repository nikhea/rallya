package handler_test

import (
	"github.com/google/uuid"

	submodel "github.com/nikhea/rallya/internal/subscription/model"
)

// proPlans grants Pro everywhere (role/apikey HTTP suites predate plans).
type proPlans struct{}

func (proPlans) EntitlementFor(_ uuid.UUID) (submodel.Entitlement, error) {
	return submodel.EntitlementForPlan(submodel.PlanPro), nil
}
