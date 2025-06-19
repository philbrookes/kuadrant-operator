package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func FindStatusCondition(conditions []metav1.Condition, conditionType string) metav1.Condition {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c
		}
	}
	return metav1.Condition{}
}

func CalculateTransitionTime(condition metav1.Condition, currentStatus metav1.ConditionStatus) metav1.Time {
	var lastTransitionTime metav1.Time

	if condition.Status != currentStatus {
		lastTransitionTime = metav1.Now()
	} else {
		lastTransitionTime = condition.LastTransitionTime
	}

	return lastTransitionTime
}

func UpdateOrAddCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
	newConditions := []metav1.Condition{}
	for _, c := range conditions {
		if c.Type != newCondition.Type {
			newConditions = append(newConditions, c)
		}
	}
	newConditions = append(newConditions, newCondition)
	return newConditions
}
