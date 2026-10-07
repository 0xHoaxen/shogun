package connectapi

import (
	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
)

// notificationTypeToAPI maps by name: taiko and the API use the same
// NOTIFICATION_TYPE_ names. A type the API does not know becomes unspecified.
func notificationTypeToAPI(t taikov1.NotificationType) apiv1.NotificationType {
	return apiv1.NotificationType(apiv1.NotificationType_value[t.String()])
}

func notificationToAPI(n *taikov1.Notification) *apiv1.Notification {
	if n == nil {
		return nil
	}
	return &apiv1.Notification{
		Id: n.GetId(), Type: notificationTypeToAPI(n.GetType()), Title: n.GetTitle(), Body: n.GetBody(),
		Link: n.GetLink(), CreatedAt: n.GetCreatedAt(), ReadAt: n.GetReadAt(),
	}
}
