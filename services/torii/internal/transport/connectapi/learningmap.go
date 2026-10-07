package connectapi

import (
	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
)

// The browser API and dojo name their enum values alike, so they are converted
// by name rather than by number: a value one side lacks becomes unspecified
// instead of silently turning into a different one.

func itemKindToAPI(k dojov1.ItemKind) apiv1.ItemKind {
	return apiv1.ItemKind(apiv1.ItemKind_value[k.String()])
}

func itemKindToDojo(k apiv1.ItemKind) dojov1.ItemKind {
	return dojov1.ItemKind(dojov1.ItemKind_value[k.String()])
}

func itemStatusToAPI(s dojov1.ItemStatus) apiv1.ItemStatus {
	return apiv1.ItemStatus(apiv1.ItemStatus_value[s.String()])
}

func itemStatusToDojo(s apiv1.ItemStatus) dojov1.ItemStatus {
	return dojov1.ItemStatus(dojov1.ItemStatus_value[s.String()])
}

func learningItemToAPI(i *dojov1.Item) *apiv1.LearningItem {
	return &apiv1.LearningItem{
		Id: i.GetId(), Title: i.GetTitle(), Kind: itemKindToAPI(i.GetKind()), Url: i.GetUrl(),
		Status: itemStatusToAPI(i.GetStatus()), StartedOn: i.GetStartedOn(), CompletedOn: i.GetCompletedOn(),
		Insight: i.GetInsight(), Version: i.GetVersion(), CreatedAt: i.GetCreatedAt(), UpdatedAt: i.GetUpdatedAt(),
	}
}

func learningItemToDojo(i *apiv1.LearningItem) *dojov1.Item {
	return &dojov1.Item{
		Id: i.GetId(), Title: i.GetTitle(), Kind: itemKindToDojo(i.GetKind()), Url: i.GetUrl(),
		Insight: i.GetInsight(), Version: i.GetVersion(),
	}
}

func learningActivityToAPI(a *dojov1.Activity) *apiv1.LearningActivity {
	return &apiv1.LearningActivity{
		Id: a.GetId(), ItemId: a.GetItemId(), Summary: a.GetSummary(), Minutes: a.GetMinutes(),
		OccurredOn: a.GetOccurredOn(), Tags: a.GetTags(), CreatedAt: a.GetCreatedAt(),
	}
}
