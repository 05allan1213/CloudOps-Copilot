package api

import "time"

func (m *MemoryQueryPort) PutRemediationPlans(incidentID string, items []RemediationPlanView) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(incidentID)
	if err != nil {
		return err
	}
	items = copyRemediationPlans(items)
	if err := validateRemediationPlanViews(items); err != nil {
		return err
	}
	m.mu.Lock()
	if m.plans == nil {
		m.plans = make(map[string][]RemediationPlanView)
	}
	m.plans[id] = items
	m.mu.Unlock()
	return nil
}

func (m *MemoryQueryPort) PutDelivery(incidentID string, item DeliveryView) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(incidentID)
	if err != nil {
		return err
	}
	copyItem := copyDelivery(&item)
	if err := validateDeliveryView(copyItem); err != nil {
		return err
	}
	m.mu.Lock()
	if m.delivery == nil {
		m.delivery = make(map[string]DeliveryView)
	}
	m.delivery[id] = *copyItem
	m.mu.Unlock()
	return nil
}

func (m *MemoryQueryPort) PutVerifications(incidentID string, items []VerificationRunView) error {
	if m == nil {
		return ErrUnavailable
	}
	id, err := ParsePublicUUID(incidentID)
	if err != nil {
		return err
	}
	items = copyVerificationRuns(items)
	if err := validateVerificationRunViews(items); err != nil {
		return err
	}
	m.mu.Lock()
	if m.verify == nil {
		m.verify = make(map[string][]VerificationRunView)
	}
	m.verify[id] = items
	m.mu.Unlock()
	return nil
}

func copyRemediationPlans(items []RemediationPlanView) []RemediationPlanView {
	if items == nil {
		return nil
	}
	result := make([]RemediationPlanView, len(items))
	for index := range items {
		result[index] = items[index]
		result[index].CanonicalManifest = append([]byte(nil), items[index].CanonicalManifest...)
		result[index].PolicySnapshot = append([]byte(nil), items[index].PolicySnapshot...)
		result[index].VerificationPlan = append([]byte(nil), items[index].VerificationPlan...)
		result[index].EvidenceBindings = append([]EvidenceBindingView(nil), items[index].EvidenceBindings...)
		if items[index].Decision != nil {
			decision := *items[index].Decision
			result[index].Decision = &decision
		}
	}
	return result
}

func copyDelivery(item *DeliveryView) *DeliveryView {
	if item == nil {
		return nil
	}
	result := *item
	result.ResourceHealth = append([]byte(nil), item.ResourceHealth...)
	result.SyncStartedAt = copyTimePointer(item.SyncStartedAt)
	result.SyncCompletedAt = copyTimePointer(item.SyncCompletedAt)
	result.DeliveryStartedAt = copyTimePointer(item.DeliveryStartedAt)
	result.DeliveryDeadlineAt = copyTimePointer(item.DeliveryDeadlineAt)
	result.DeliveryCompletedAt = copyTimePointer(item.DeliveryCompletedAt)
	result.LastObservedAt = copyTimePointer(item.LastObservedAt)
	return &result
}

func copyVerificationRuns(items []VerificationRunView) []VerificationRunView {
	if items == nil {
		return nil
	}
	result := make([]VerificationRunView, len(items))
	for runIndex := range items {
		result[runIndex] = items[runIndex]
		result[runIndex].StartedAt = copyTimePointer(items[runIndex].StartedAt)
		result[runIndex].CompletedAt = copyTimePointer(items[runIndex].CompletedAt)
		result[runIndex].CommonWindow.SuccessSince = copyTimePointer(items[runIndex].CommonWindow.SuccessSince)
		result[runIndex].CommonWindow.CompletedAt = copyTimePointer(items[runIndex].CommonWindow.CompletedAt)
		result[runIndex].Checks = make([]VerificationCheckView, len(items[runIndex].Checks))
		for checkIndex := range items[runIndex].Checks {
			check := items[runIndex].Checks[checkIndex]
			result[runIndex].Checks[checkIndex] = check
			result[runIndex].Checks[checkIndex].Expected = append([]byte(nil), check.Expected...)
			result[runIndex].Checks[checkIndex].Observed = append([]byte(nil), check.Observed...)
			result[runIndex].Checks[checkIndex].Threshold = copyFloatPointer(check.Threshold)
			result[runIndex].Checks[checkIndex].FirstCheckedAt = copyTimePointer(check.FirstCheckedAt)
			result[runIndex].Checks[checkIndex].LastCheckedAt = copyTimePointer(check.LastCheckedAt)
			result[runIndex].Checks[checkIndex].PassedAt = copyTimePointer(check.PassedAt)
			result[runIndex].Checks[checkIndex].ConsecutiveSuccessSince = copyTimePointer(check.ConsecutiveSuccessSince)
			result[runIndex].Checks[checkIndex].Samples = make([]VerificationSampleView, len(check.Samples))
			for sampleIndex := range check.Samples {
				sample := check.Samples[sampleIndex]
				result[runIndex].Checks[checkIndex].Samples[sampleIndex] = sample
				result[runIndex].Checks[checkIndex].Samples[sampleIndex].Observed = append([]byte(nil), sample.Observed...)
				result[runIndex].Checks[checkIndex].Samples[sampleIndex].WindowStartAt = copyTimePointer(sample.WindowStartAt)
				result[runIndex].Checks[checkIndex].Samples[sampleIndex].WindowEndAt = copyTimePointer(sample.WindowEndAt)
			}
		}
	}
	return result
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func copyFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
