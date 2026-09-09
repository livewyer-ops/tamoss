package consoleapi

import (
	"context"
	"log/slog"
)

type CommandAuditRecord struct {
	Subject    string
	Roles      []Role
	AuthMethod string
	RequestID  string
	Namespace  string
	Instance   string
	Command    string
	TargetKind string
	TargetName string
	TargetUID  string
	Outcome    string
	Reason     string
	HTTPStatus int
	Replay     bool
}

type CommandAuditor interface {
	Record(context.Context, CommandAuditRecord)
}

type slogCommandAuditor struct {
	logger *slog.Logger
}

func NewSlogCommandAuditor(logger *slog.Logger) CommandAuditor {
	return &slogCommandAuditor{logger: logger}
}

func (a *slogCommandAuditor) Record(ctx context.Context, record CommandAuditRecord) {
	if a == nil || a.logger == nil {
		return
	}
	roles := make([]string, 0, len(record.Roles))
	for _, role := range record.Roles {
		roles = append(roles, string(role))
	}
	a.logger.InfoContext(
		ctx,
		"Console command audit",
		"audit", true,
		"subject", record.Subject,
		"roles", roles,
		"auth_method", record.AuthMethod,
		"request_id", record.RequestID,
		"namespace", record.Namespace,
		"instance", record.Instance,
		"command", record.Command,
		"target_kind", record.TargetKind,
		"target_name", record.TargetName,
		"target_uid", record.TargetUID,
		"outcome", record.Outcome,
		"reason", record.Reason,
		"http_status", record.HTTPStatus,
		"replay", record.Replay,
	)
}
