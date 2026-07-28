package remediation

// GitOpsBranch returns the branch identity accepted by the external GitOps
// required check. Plan validation already guarantees a lowercase UUID and
// a lowercase 64-character canonical plan hash before delivery reaches this path.
func GitOpsBranch(incidentPublicID, canonicalPlanHash string) string {
	return "cloudops/incident-" + incidentPublicID + "/plan-" + canonicalPlanHash
}
