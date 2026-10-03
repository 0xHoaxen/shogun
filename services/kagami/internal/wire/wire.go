// Package wire converts between domain statuses and the kagami.v1 proto enums.
package wire

import (
	"strings"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

const (
	jobStatusPrefix     = "JOB_STATUS_"
	contactStatusPrefix = "CONTACT_STATUS_"
)

// JobStatusToProto returns the enum for s, or unspecified for an unknown status.
func JobStatusToProto(s domain.JobStatus) kagamiv1.JobStatus {
	return kagamiv1.JobStatus(kagamiv1.JobStatus_value[jobStatusPrefix+strings.ToUpper(string(s))])
}

// JobStatusFromProto returns the domain status for p. It reports false for
// unspecified or unknown enum values.
func JobStatusFromProto(p kagamiv1.JobStatus) (domain.JobStatus, bool) {
	name, ok := strings.CutPrefix(p.String(), jobStatusPrefix)
	if !ok || p == kagamiv1.JobStatus_JOB_STATUS_UNSPECIFIED {
		return "", false
	}
	s := domain.JobStatus(strings.ToLower(name))
	return s, s.Valid()
}

// ContactStatusToProto returns the enum for s, or unspecified for an unknown
// status.
func ContactStatusToProto(s domain.ContactStatus) kagamiv1.ContactStatus {
	return kagamiv1.ContactStatus(kagamiv1.ContactStatus_value[contactStatusPrefix+strings.ToUpper(string(s))])
}

// ContactStatusFromProto returns the domain status for p. It reports false for
// unspecified or unknown enum values.
func ContactStatusFromProto(p kagamiv1.ContactStatus) (domain.ContactStatus, bool) {
	name, ok := strings.CutPrefix(p.String(), contactStatusPrefix)
	if !ok || p == kagamiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED {
		return "", false
	}
	s := domain.ContactStatus(strings.ToLower(name))
	return s, s.Valid()
}
