package sweep

import (
	"reflect"
	"testing"
	"time"
)

func TestSelectStale(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	recent := now.Add(-10 * 24 * time.Hour)
	reported := now.Add(-80 * 24 * time.Hour)
	tests := []struct {
		name       string
		deliveries []CourseDelivery
		want       []Candidate
	}{
		{
			name: "deadline exactly at cutoff after course close and educator report",
			deliveries: []CourseDelivery{{
				CourseID: "math-201", DeliveryEndedAt: old, EducatorReportedAt: reported,
				Learners: []Learner{{LearnerID: "learner-7", Deadline: old, ObjectKey: "math-201/learner-7.json"}},
			}},
			want: []Candidate{{CourseID: "math-201", LearnerID: "learner-7", ObjectKey: "math-201/learner-7.json"}},
		},
		{
			name: "educator report still pending",
			deliveries: []CourseDelivery{{
				CourseID: "physics-101", DeliveryEndedAt: old,
				Learners: []Learner{{LearnerID: "learner-8", Deadline: old, ObjectKey: "physics-101/learner-8.json"}},
			}},
			want: nil,
		},
		{
			name: "learner deadline inside retention window",
			deliveries: []CourseDelivery{{
				CourseID: "history-101", DeliveryEndedAt: old, EducatorReportedAt: reported,
				Learners: []Learner{{LearnerID: "learner-9", Deadline: recent, ObjectKey: "history-101/learner-9.json"}},
			}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectStale(tt.deliveries, now, 90*24*time.Hour)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SelectStale() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
