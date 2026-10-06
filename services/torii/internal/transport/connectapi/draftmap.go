package connectapi

import (
	"strings"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
)

// The browser API and fude name their enum values alike, so they are converted
// by name rather than by number: a value one side lacks becomes unspecified
// instead of silently turning into a different one.

func draftKindToAPI(k fudev1.DraftKind) apiv1.DraftKind {
	return apiv1.DraftKind(apiv1.DraftKind_value[k.String()])
}

func draftKindToFude(k apiv1.DraftKind) fudev1.DraftKind {
	return fudev1.DraftKind(fudev1.DraftKind_value[k.String()])
}

func draftStateToAPI(s fudev1.DraftState) apiv1.DraftState {
	return apiv1.DraftState(apiv1.DraftState_value[s.String()])
}

func draftStateToFude(s apiv1.DraftState) fudev1.DraftState {
	return fudev1.DraftState(fudev1.DraftState_value[s.String()])
}

// The API's channel values carry a DRAFT_ prefix fude's do not.
const draftPrefix = "DRAFT_"

func draftChannelToAPI(c fudev1.Channel) apiv1.DraftChannel {
	return apiv1.DraftChannel(apiv1.DraftChannel_value[draftPrefix+c.String()])
}

func draftChannelToFude(c apiv1.DraftChannel) fudev1.Channel {
	return fudev1.Channel(fudev1.Channel_value[strings.TrimPrefix(c.String(), draftPrefix)])
}

const (
	apiTargetPrefix  = "DRAFT_TARGET_"
	fudeTargetPrefix = "TARGET_TYPE_"
)

// draftTargetToAPI drops targets the browser does not work with: a learning
// activity has no screen yet.
func draftTargetToAPI(t fudev1.TargetType) apiv1.DraftTarget {
	suffix := strings.TrimPrefix(t.String(), fudeTargetPrefix)
	return apiv1.DraftTarget(apiv1.DraftTarget_value[apiTargetPrefix+suffix])
}

func draftTargetToFude(t apiv1.DraftTarget) fudev1.TargetType {
	suffix := strings.TrimPrefix(t.String(), apiTargetPrefix)
	return fudev1.TargetType(fudev1.TargetType_value[fudeTargetPrefix+suffix])
}

func draftToAPI(d *fudev1.Draft) *apiv1.Draft {
	if d == nil {
		return nil
	}
	return &apiv1.Draft{
		Id: d.GetId(), Kind: draftKindToAPI(d.GetKind()), Target: draftTargetToAPI(d.GetTargetType()),
		TargetId: d.GetTargetId(), Channel: draftChannelToAPI(d.GetChannel()), State: draftStateToAPI(d.GetState()),
		CurrentVersion: d.GetCurrentVersion(), Recipient: d.GetRecipient(), FailureReason: d.GetFailureReason(),
		Version: d.GetVersion(), Subject: d.GetSubject(), Preview: d.GetPreview(),
		CreatedAt: d.GetCreatedAt(), UpdatedAt: d.GetUpdatedAt(),
	}
}

func draftVersionToAPI(v *fudev1.DraftVersion) *apiv1.DraftVersion {
	if v == nil {
		return nil
	}
	return &apiv1.DraftVersion{
		Version: v.GetVersion(), Subject: v.GetSubject(), Body: v.GetBody(),
		Author:       apiv1.VersionAuthor(apiv1.VersionAuthor_value[v.GetCreatedBy().String()]),
		ExtraContext: v.GetExtraContext(), CreatedAt: v.GetCreatedAt(),
	}
}
