package service

import (
	"context"
	"strings"

	"server-web/internal/copilot/nlu"
)

func (s *Service) loadSessionContext(ctx context.Context, sessionID string) ([]ChatHistoryItem, map[string]string) {
	if s.contextManager == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	history, _ := s.contextManager.LoadHistory(ctx, sessionID)
	entities, _ := s.contextManager.LoadEntities(ctx, sessionID)
	return history, entities
}

func (s *Service) saveSessionContext(ctx context.Context, sessionID string, oldEntities, newEntities map[string]string) {
	if s.contextManager == nil || strings.TrimSpace(sessionID) == "" || len(newEntities) == 0 {
		return
	}
	merged := mergeEntities(oldEntities, newEntities)
	_ = s.contextManager.SaveEntities(ctx, sessionID, merged, s.sessionTTL)
}

func applyContextEntities(result nlu.Result, entities map[string]string) nlu.Result {
	if len(entities) == 0 {
		return result
	}
	result.Entities = mergeEntities(entities, result.Entities)
	for i := range result.Intents {
		result.Intents[i].Entities = mergeEntities(entities, result.Intents[i].Entities)
	}
	return result
}
