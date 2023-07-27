package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	observerv1alpha1 "github.com/observer-io/observer/api/v1alpha1"
)

const (
	ConditionTypeReady = "Ready"
	ConditionTypeError = "Error"
)

// SetReadyCondition - shortcut to set ready condition to true
func SetReadyCondition(appStatus *observerv1alpha1.ObserverStatus, reason, message string) {
	setCondition(appStatus, ConditionTypeReady, metav1.ConditionTrue, reason, message)
}

// SetNotReadyCondition - shortcut to set ready condition to false
func SetNotReadyCondition(appStatus *observerv1alpha1.ObserverStatus, reason, message string) {
	setCondition(appStatus, ConditionTypeReady, metav1.ConditionFalse, reason, message)
}

// SetReadyUnknownCondition - shortcut to set ready condition to unknown
func SetReadyUnknownCondition(appStatus *observerv1alpha1.ObserverStatus, reason, message string) {
	setCondition(appStatus, ConditionTypeReady, metav1.ConditionUnknown, reason, message)
}

// SetErrorCondition - shortcut to set error condition
func SetErrorCondition(appStatus *observerv1alpha1.ObserverStatus, reason, message string) {
	setCondition(appStatus, ConditionTypeError, metav1.ConditionTrue, reason, message)
}

// ClearErrorCondition - shortcut to set error condition
func ClearErrorCondition(appStatus *observerv1alpha1.ObserverStatus) {
	setCondition(appStatus, ConditionTypeError, metav1.ConditionFalse, "NoError", "No error seen")
}

func setCondition(appStatus *observerv1alpha1.ObserverStatus, ctype string, status metav1.ConditionStatus, reason, message string) {
	for i, c := range appStatus.Conditions {
		if c.Type == ctype {
			if c.Status == status && c.Reason == reason && c.Message == message {
				return
			}
			now := metav1.Now()
			c.LastTransitionTime = now
			c.Status = status
			c.Reason = reason
			c.Message = message
			appStatus.Conditions[i] = c
			return
		}
	}
	addCondition(appStatus, ctype, status, reason, message)
}

func addCondition(appStatus *observerv1alpha1.ObserverStatus, ctype string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	c := metav1.Condition{
		Type:               ctype,
		LastTransitionTime: now,
		Status:             status,
		Reason:             reason,
		Message:            message,
	}
	appStatus.Conditions = append(appStatus.Conditions, c)
}
