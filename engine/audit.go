package engine

import "time"

type AuditEntry struct {
	Type      string
	Details   string
	TimeStamp time.Time
}

type AuditService struct {
	events []AuditEntry
}

func NewAuditService() *AuditService {
	return &AuditService{
		events: []AuditEntry{},
	}
}

func (a *AuditService) RecordEvent(eventType, details string) {
	a.events = append(a.events, AuditEntry{
		Type:      eventType,
		Details:   details,
		TimeStamp: time.Now(),
	})
}

func (a *AuditService) GetEvents() []AuditEntry {
	return a.events
}
