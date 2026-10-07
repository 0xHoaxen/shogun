// Package wire converts between dojo's domain enums and the generated proto
// enums.
package wire

import (
	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
)

var kindToProto = map[domain.ItemKind]dojov1.ItemKind{
	domain.KindCourse:  dojov1.ItemKind_ITEM_KIND_COURSE,
	domain.KindBook:    dojov1.ItemKind_ITEM_KIND_BOOK,
	domain.KindProject: dojov1.ItemKind_ITEM_KIND_PROJECT,
	domain.KindSkill:   dojov1.ItemKind_ITEM_KIND_SKILL,
}

var statusToProto = map[domain.ItemStatus]dojov1.ItemStatus{
	domain.StatusPlanned:    dojov1.ItemStatus_ITEM_STATUS_PLANNED,
	domain.StatusInProgress: dojov1.ItemStatus_ITEM_STATUS_IN_PROGRESS,
	domain.StatusDone:       dojov1.ItemStatus_ITEM_STATUS_DONE,
}

// KindToProto returns the proto kind, or unspecified for an unknown one.
func KindToProto(k domain.ItemKind) dojov1.ItemKind { return kindToProto[k] }

// KindFromProto returns the domain kind, or "" for unspecified.
func KindFromProto(k dojov1.ItemKind) domain.ItemKind {
	for d, p := range kindToProto {
		if p == k {
			return d
		}
	}
	return ""
}

// StatusToProto returns the proto status, or unspecified for an unknown one.
func StatusToProto(s domain.ItemStatus) dojov1.ItemStatus { return statusToProto[s] }

// StatusFromProto returns the domain status, or "" for unspecified.
func StatusFromProto(s dojov1.ItemStatus) domain.ItemStatus {
	for d, p := range statusToProto {
		if p == s {
			return d
		}
	}
	return ""
}
