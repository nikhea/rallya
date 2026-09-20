package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	checkindto "github.com/nikhea/rallya/internal/checkin/dto"
	checkinmodel "github.com/nikhea/rallya/internal/checkin/model"
)

func TestHTTPDoorRevert(t *testing.T) {
	f := newDoorFixture(t)
	base := "/api/v1/orgs/door/events/gala/checkin"
	owner, member := "downer@test.com", "dmember@test.com"

	// Promote member to ADMIN for revert rights? No — MEMBER must 403.
	// Check the row in first (manual fallback).
	code, _ := doorReq(t, f, owner, "POST", base, `{"attendeeId":"`+f.attID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("pre-checkin: want 200, got %d", code)
	}

	// ADMIN revert lands REVERTED.
	code, body := doorReq(t, f, owner, "POST", base+"/revert", `{"attendeeId":"`+f.attID+`"}`)
	var r checkindto.ScanResult
	_ = json.Unmarshal(body, &r)
	if code != http.StatusOK || r.Outcome != string(checkinmodel.OutcomeReverted) {
		t.Fatalf("revert: want 200 REVERTED, got %d %s (%s)", code, r.Outcome, body)
	}

	// Second revert: 422, nothing left to undo.
	code, _ = doorReq(t, f, owner, "POST", base+"/revert", `{"attendeeId":"`+f.attID+`"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("double revert: want 422, got %d", code)
	}

	// MEMBER revert: 403 (update right is ADMIN+).
	code, _ = doorReq(t, f, member, "POST", base+"/revert", `{"attendeeId":"`+f.attID+`"}`)
	if code != http.StatusForbidden {
		t.Fatalf("member revert: want 403, got %d", code)
	}

	// Anonymous: 401.
	code, _ = doorReq(t, f, "", "POST", base+"/revert", `{"attendeeId":"`+f.attID+`"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anon revert: want 401, got %d", code)
	}
}
