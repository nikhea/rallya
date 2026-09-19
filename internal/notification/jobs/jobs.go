// Package jobs defines River email jobs and workers.
// Auth flows enqueue; the in-process River client works them.
package jobs

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/internal/notification"
	"github.com/nikhea/rallya/internal/notification/mailer"
	"github.com/nikhea/rallya/internal/notification/templates"
)

// EmailQueue carries all outbound mail.
const EmailQueue = "emails"

// ---------- job args ----------

// SendVerificationEmailArgs renders the verify template (OTP + link).
// OTP is the raw 6-digit code; only its hash is persisted elsewhere.
type SendVerificationEmailArgs struct {
	UserID     uuid.UUID `json:"userId"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	OTP        string    `json:"otp"`
	VerifyLink string    `json:"verifyLink"`
}

func (SendVerificationEmailArgs) Kind() string { return "send_verification_email" }

// SendPasswordResetEmailArgs renders the reset template (link only).
type SendPasswordResetEmailArgs struct {
	UserID    uuid.UUID `json:"userId"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	ResetLink string    `json:"resetLink"`
}

func (SendPasswordResetEmailArgs) Kind() string { return "send_password_reset_email" }

// SendWelcomeEmailArgs renders the post-verification welcome email.
type SendWelcomeEmailArgs struct {
	UserID    uuid.UUID `json:"userId"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	LoginLink string    `json:"loginLink"`
}

func (SendWelcomeEmailArgs) Kind() string { return "send_welcome_email" }

// SendOrgInviteEmailArgs renders the organization invitation email.
type SendOrgInviteEmailArgs struct {
	OrgID       uuid.UUID `json:"orgId"`
	OrgName     string    `json:"orgName"`
	OrgSlug     string    `json:"orgSlug"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	InviterName string    `json:"inviterName"`
	InviteLink  string    `json:"inviteLink"`
}

func (SendOrgInviteEmailArgs) Kind() string { return "send_org_invite_email" }

// ---------- workers ----------

// VerificationEmailWorker sends the verify email.
type VerificationEmailWorker struct {
	river.WorkerDefaults[SendVerificationEmailArgs]
}

func (w *VerificationEmailWorker) Work(ctx context.Context, job *river.Job[SendVerificationEmailArgs]) error {
	a := job.Args
	r := templates.RenderVerification(templates.VerifyEmailData{
		AppName: notification.AppName(), Name: a.Name, OTP: a.OTP, VerifyLink: a.VerifyLink,
	})
	slog.Info("sending verification email", "to", a.Email, "job", job.ID)
	if err := mailer.Send(a.Email, r.Subject, r.Text, r.HTML); err != nil {
		return err // River retries with backoff
	}
	return nil
}

// PasswordResetEmailWorker sends the reset email.
type PasswordResetEmailWorker struct {
	river.WorkerDefaults[SendPasswordResetEmailArgs]
}

func (w *PasswordResetEmailWorker) Work(ctx context.Context, job *river.Job[SendPasswordResetEmailArgs]) error {
	a := job.Args
	r := templates.RenderPasswordReset(templates.ResetPasswordData{
		AppName: notification.AppName(), Name: a.Name, ResetLink: a.ResetLink,
	})
	slog.Info("sending password reset email", "to", a.Email, "job", job.ID)
	if err := mailer.Send(a.Email, r.Subject, r.Text, r.HTML); err != nil {
		return err
	}
	return nil
}

// WelcomeEmailWorker sends the post-verification welcome email.
type WelcomeEmailWorker struct {
	river.WorkerDefaults[SendWelcomeEmailArgs]
}

func (w *WelcomeEmailWorker) Work(ctx context.Context, job *river.Job[SendWelcomeEmailArgs]) error {
	a := job.Args
	r := templates.RenderWelcome(templates.WelcomeData{
		AppName: notification.AppName(), Name: a.Name, LoginLink: a.LoginLink,
	})
	slog.Info("sending welcome email", "to", a.Email, "job", job.ID)
	if err := mailer.Send(a.Email, r.Subject, r.Text, r.HTML); err != nil {
		return err
	}
	return nil
}

// OrgInviteEmailWorker sends the organization invitation email.
type OrgInviteEmailWorker struct {
	river.WorkerDefaults[SendOrgInviteEmailArgs]
}

func (w *OrgInviteEmailWorker) Work(ctx context.Context, job *river.Job[SendOrgInviteEmailArgs]) error {
	a := job.Args
	r := templates.RenderOrgInvite(templates.OrgInviteData{
		AppName: notification.AppName(), Name: a.Name, OrgName: a.OrgName,
		Role: a.Role, InviterName: a.InviterName, InviteLink: a.InviteLink,
	})
	slog.Info("sending org invite email", "to", a.Email, "org", a.OrgSlug, "job", job.ID)
	if err := mailer.Send(a.Email, r.Subject, r.Text, r.HTML); err != nil {
		return err
	}
	return nil
}

// AddAll registers every email worker. Panics on misconfiguration
// (fail-fast at boot, per River convention).
func AddAll(workers *river.Workers) {
	river.AddWorker(workers, &VerificationEmailWorker{})
	river.AddWorker(workers, &PasswordResetEmailWorker{})
	river.AddWorker(workers, &WelcomeEmailWorker{})
	river.AddWorker(workers, &OrgInviteEmailWorker{})
}

// ---------- enqueue contract ----------

// Enqueuer abstracts job insertion so services stay testable.
// Production uses RiverEnqueuer; tests use FakeEnqueuer.
type Enqueuer interface {
	// EnqueueTx inserts within a GORM transaction (unwraps *sql.Tx).
	// Falls back to a plain insert when tx is nil or not transactional.
	EnqueueTx(ctx context.Context, tx *gorm.DB, args river.JobArgs) error
	// Enqueue inserts outside any transaction (post-commit notifications).
	Enqueue(ctx context.Context, args river.JobArgs) error
}

// RiverEnqueuer implements Enqueuer over a River client.
// TTx is *sql.Tx (riverdatabasesql driver).
type RiverEnqueuer struct {
	client *river.Client[*sql.Tx]
	queue  string
}

// NewRiverEnqueuer wraps client; jobs land on queue (usually EmailQueue).
func NewRiverEnqueuer(client *river.Client[*sql.Tx], queue string) *RiverEnqueuer {
	return &RiverEnqueuer{client: client, queue: queue}
}

func (e *RiverEnqueuer) opts() *river.InsertOpts {
	return &river.InsertOpts{Queue: e.queue}
}

// EnqueueTx implements Enqueuer.
func (e *RiverEnqueuer) EnqueueTx(ctx context.Context, tx *gorm.DB, args river.JobArgs) error {
	if tx != nil {
		if sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx); ok {
			_, err := e.client.InsertTx(ctx, sqlTx, args, e.opts())
			return err
		}
	}
	return e.Enqueue(ctx, args)
}

// Enqueue implements Enqueuer.
func (e *RiverEnqueuer) Enqueue(ctx context.Context, args river.JobArgs) error {
	_, err := e.client.Insert(ctx, args, e.opts())
	return err
}

// ---------- test fake ----------

// FakeEnqueuer records enqueued args for assertions.
type FakeEnqueuer struct {
	Jobs []river.JobArgs
}

// EnqueueTx implements Enqueuer (records only).
func (f *FakeEnqueuer) EnqueueTx(_ context.Context, _ *gorm.DB, args river.JobArgs) error {
	f.Jobs = append(f.Jobs, args)
	return nil
}

// Enqueue implements Enqueuer (records only).
func (f *FakeEnqueuer) Enqueue(_ context.Context, args river.JobArgs) error {
	f.Jobs = append(f.Jobs, args)
	return nil
}

// OfKind filters recorded jobs by Kind().
func (f *FakeEnqueuer) OfKind(kind string) []river.JobArgs {
	var out []river.JobArgs
	for _, j := range f.Jobs {
		if j.Kind() == kind {
			out = append(out, j)
		}
	}
	return out
}

var (
	_ Enqueuer = (*RiverEnqueuer)(nil)
	_ Enqueuer = (*FakeEnqueuer)(nil)
)
