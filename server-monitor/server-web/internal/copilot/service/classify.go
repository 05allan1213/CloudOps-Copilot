package service

import (
	"context"

	"server-web/internal/copilot/nlu"
)

func defaultClassifier(classifier *nlu.Classifier) *nlu.Classifier {
	if classifier != nil {
		return classifier
	}
	return nlu.NewClassifier()
}

func (s *Service) classifyWithFallback(ctx context.Context, message string, parsed nlu.Result) nlu.Result {
	if s.llm == nil || parsed.Confidence >= 0.6 {
		if parsed.Source == "" {
			if parsed.Confidence >= 0.6 {
				parsed.Source = "rule"
			} else {
				parsed.Source = "rule-low"
			}
		}
		return parsed
	}

	if s.toolsClassifyEnabled && len(s.toolDefs) > 0 {
		classifierWithTools, ok := s.llm.(LLMClassifierWithTools)
		if ok {
			if len(parsed.Intents) > 0 || nlu.HasMultiIntentHints(message) {
				toolResult, err := classifierWithTools.ClassifyWithToolsMulti(ctx, message, s.toolDefs)
				if err == nil && (toolResult.Intent != "" || len(toolResult.Intents) > 0) {
					toolResult.Source = "tools"
					if toolResult.Entities == nil {
						toolResult.Entities = map[string]string{}
					}
					return toolResult
				}
			} else {
				toolResult, err := classifierWithTools.ClassifyWithTools(ctx, message, s.toolDefs)
				if err == nil && toolResult.Intent != "" {
					toolResult.Source = "tools"
					if toolResult.Entities == nil {
						toolResult.Entities = map[string]string{}
					}
					return toolResult
				}
			}
		}
	}

	llmResult, err := s.llm.Classify(ctx, message)
	if err != nil {
		return parsed
	}
	if llmResult.Intent == "" {
		return parsed
	}
	if llmResult.Entities == nil {
		llmResult.Entities = map[string]string{}
	}
	llmResult.Source = "json"
	return llmResult
}
