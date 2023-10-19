package k8sutil

import (
	kbatchv1 "k8s.io/api/batch/v1"
	kv1 "k8s.io/api/core/v1"
)

type JobState string

const (
	JobDoesNotExist JobState = ""
	JobActive                = "Running"
	JobSucceeded             = "Succeeded"
	JobFailed                = "Failed"
)

func (js JobState) IsComplete() bool {
	return js == JobSucceeded || js == JobFailed
}

func GetJobState(j *kbatchv1.Job) JobState {
	if j == nil {
		return JobDoesNotExist
	}
	if j.Status.Active > 0 {
		return JobActive
	}
	if c := getJobCondition(kbatchv1.JobFailed, j.Status.Conditions); c != nil && c.Status == kv1.ConditionTrue {
		return JobFailed
	}
	if c := getJobCondition(kbatchv1.JobComplete, j.Status.Conditions); c != nil && c.Status == kv1.ConditionTrue {
		return JobSucceeded
	}
	return JobActive // i guess?
}

func getJobCondition(jct kbatchv1.JobConditionType, conds []kbatchv1.JobCondition) *kbatchv1.JobCondition {
	for _, cond := range conds {
		if cond.Type == jct {
			return &cond
		}
	}
	return nil
}
