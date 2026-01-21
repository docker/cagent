package runtime

// IsValidResumeType validates confirmation values coming from /resume
func IsValidResumeType(t ResumeType) bool {
	switch t {
	case ResumeTypeApprove,
		ResumeTypeApproveSession,
		ResumeTypeReject:
		return true
	default:
		return false
	}
}

// ValidResumeTypes returns all allowed confirmation values
func ValidResumeTypes() []ResumeType {
	return []ResumeType{
		ResumeTypeApprove,
		ResumeTypeApproveSession,
		ResumeTypeReject,
	}
}
