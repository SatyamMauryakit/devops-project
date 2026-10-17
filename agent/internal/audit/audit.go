// internal/audit/audit.go
// Append-only audit log for every remediation action.
//
// Every action — whether applied, rejected, or rolled back — is recorded with:
//   - Who approved it (Slack username or WhatsApp number)
//   - The reasoning (LLM diagnosis)
//   - The evidence used
//   - The action taken
//   - The result (success, failure, auto-rolled-back)
//   - The rollback plan
//
// Storage: MongoDB collection "audit_records" in the "sentinel" database.
// Fallback: if MongoDB is unreachable, the record is written to a local
// newline-delimited JSON file at /var/log/sentinel/audit.jsonl.
//
// Records are never deleted or updated — the collection is append-only.

package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	dbName         = "sentinel"
	collectionName = "audit_records"
	fallbackLogDir = "/var/log/sentinel"
	fallbackLogFile = "audit.jsonl"
)

// Record is one audit entry. All fields are exported for MongoDB serialisation.
type Record struct {
	// Timestamp is when the record was written (UTC).
	Timestamp time.Time `bson:"timestamp"      json:"timestamp"`
	// WorkflowID is the Temporal workflow ID for this incident.
	WorkflowID string `bson:"workflow_id"    json:"workflow_id"`
	// AlertName is the Prometheus alert that triggered the incident.
	AlertName string `bson:"alert_name"     json:"alert_name"`
	// ApprovedBy is the Slack username or WhatsApp number of the approver.
	// Empty for rejected or auto-rolled-back records.
	ApprovedBy string `bson:"approved_by"    json:"approved_by"`
	// Decision is: approve | reject | ignore | timeout | auto_rollback.
	Decision string `bson:"decision"       json:"decision"`
	// Action is the remediation action that was proposed.
	Action string `bson:"action"         json:"action"`
	// Result is: applied | rejected | auto_rolled_back | failed.
	Result string `bson:"result"         json:"result"`
	// Reasoning is the LLM's explanation for the proposed action.
	Reasoning string `bson:"reasoning"      json:"reasoning"`
	// EvidenceUsed are the log/metric lines the LLM cited.
	EvidenceUsed []string `bson:"evidence_used"  json:"evidence_used"`
	// RollbackPlan describes how to undo this action manually.
	RollbackPlan string `bson:"rollback_plan"  json:"rollback_plan"`
	// Error is set when the action failed or was auto-rolled-back.
	Error string `bson:"error,omitempty" json:"error,omitempty"`
}

// Logger writes audit records to MongoDB with a file fallback.
type Logger struct {
	mongo *mongo.Client
	// mongoURI is kept only for reconnection; never logged.
	mongoURI string
}

// New creates an audit Logger. mongoURI must not be empty.
// The connection is established lazily on the first Write call.
func New(mongoURI string) *Logger {
	return &Logger{mongoURI: mongoURI}
}

// Write appends an audit record. If MongoDB is unreachable, it falls back
// to writing a JSON line to the local fallback file.
// This method never returns an error to the caller — a failed audit write
// is logged but must not block the remediation flow.
func (l *Logger) Write(ctx context.Context, rec Record) {
	rec.Timestamp = time.Now().UTC()

	if err := l.writeToMongo(ctx, rec); err != nil {
		slog.Warn("audit: MongoDB write failed, falling back to file",
			"workflowID", rec.WorkflowID,
			"error", err.Error(),
		)
		l.writeToFile(rec)
	} else {
		slog.Info("audit record written",
			"workflowID", rec.WorkflowID,
			"decision", rec.Decision,
			"action", rec.Action,
			"result", rec.Result,
		)
	}
}

func (l *Logger) writeToMongo(ctx context.Context, rec Record) error {
	if l.mongo == nil {
		client, err := mongo.Connect(ctx,
			options.Client().ApplyURI(l.mongoURI).
				SetServerSelectionTimeout(5*time.Second),
		)
		if err != nil {
			return fmt.Errorf("connect to MongoDB: %w", err)
		}
		l.mongo = client
	}

	coll := l.mongo.Database(dbName).Collection(collectionName)

	// Convert the struct to a BSON document. Using bson.Marshal ensures
	// the field names use the bson tags defined on Record.
	doc, err := bson.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}

	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		// Mark the client as nil so we reconnect on the next attempt.
		l.mongo = nil
		return fmt.Errorf("insert audit record: %w", err)
	}

	return nil
}

func (l *Logger) writeToFile(rec Record) {
	line, err := json.Marshal(rec)
	if err != nil {
		slog.Error("audit: cannot marshal record to JSON", "error", err.Error())
		return
	}

	path := filepath.Join(fallbackLogDir, fallbackLogFile)

	// Create the directory if it does not exist.
	if err := os.MkdirAll(fallbackLogDir, 0o750); err != nil {
		slog.Error("audit: cannot create log directory",
			"path", fallbackLogDir,
			"error", err.Error(),
		)
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		slog.Error("audit: cannot open fallback log file",
			"path", path,
			"error", err.Error(),
		)
		return
	}
	defer f.Close()

	fmt.Fprintln(f, string(line))
}
