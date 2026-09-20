package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Workers run with the mailer in log-only mode here (no SMTP env), which
// exercises render + send plumbing without a mail server.
func TestVerificationEmailWorker(t *testing.T) {
	w := &VerificationEmailWorker{}
	job := &river.Job[SendVerificationEmailArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: SendVerificationEmailArgs{
		UserID: uuid.New(), Email: "jane@test.com", Name: "Jane",
		OTP: "482910", VerifyLink: "http://localhost:8080/verify?token=abc",
	}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("work: %v", err)
	}
}

func TestPasswordResetEmailWorker(t *testing.T) {
	w := &PasswordResetEmailWorker{}
	job := &river.Job[SendPasswordResetEmailArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: SendPasswordResetEmailArgs{
		UserID: uuid.New(), Email: "jane@test.com", Name: "Jane",
		ResetLink: "http://localhost:8080/reset?token=abc",
	}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("work: %v", err)
	}
}

func TestWelcomeEmailWorker(t *testing.T) {
	w := &WelcomeEmailWorker{}
	job := &river.Job[SendWelcomeEmailArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: SendWelcomeEmailArgs{
		UserID: uuid.New(), Email: "jane@test.com", Name: "Jane",
		LoginLink: "http://localhost:8080",
	}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("work: %v", err)
	}
}

func TestOrgInviteEmailWorker(t *testing.T) {
	w := &OrgInviteEmailWorker{}
	job := &river.Job[SendOrgInviteEmailArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: SendOrgInviteEmailArgs{
		OrgID: uuid.New(), OrgName: "Acme", OrgSlug: "acme",
		Email: "jane@test.com", Name: "jane", Role: "MEMBER",
		InviterName: "John", InviteLink: "http://localhost:8080/invites/accept?token=abc",
	}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("work: %v", err)
	}
}

func TestAddAllRegisters(t *testing.T) {
	workers := river.NewWorkers()
	AddAll(workers) // panics on misconfiguration
}

func TestFakeEnqueuerRecords(t *testing.T) {
	f := &FakeEnqueuer{}
	ctx := context.Background()
	args := SendWelcomeEmailArgs{Email: "a@test.com"}
	if err := f.Enqueue(ctx, args); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := f.EnqueueTx(ctx, nil, args); err != nil {
		t.Fatalf("enqueueTx: %v", err)
	}
	if len(f.Jobs) != 2 || len(f.OfKind("send_welcome_email")) != 2 {
		t.Fatalf("unexpected jobs: %+v", f.Jobs)
	}
	if len(f.OfKind("send_verification_email")) != 0 {
		t.Fatal("expected no verification jobs")
	}
}
