package sweep

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

type CourseDelivery struct {
	CourseID           string    `json:"course_id"`
	DeliveryEndedAt    time.Time `json:"delivery_ended_at"`
	EducatorReportedAt time.Time `json:"educator_reported_at"`
	Learners           []Learner `json:"learners"`
}

type Learner struct {
	LearnerID string    `json:"learner_id"`
	Deadline  time.Time `json:"deadline"`
	ObjectKey string    `json:"object_key"`
}

type Candidate struct {
	CourseID  string `json:"course_id"`
	LearnerID string `json:"learner_id"`
	ObjectKey string `json:"object_key"`
}

type Report struct {
	Scanned int         `json:"scanned"`
	Deleted int         `json:"deleted"`
	Records []Candidate `json:"records"`
}

func SelectStale(deliveries []CourseDelivery, now time.Time, retention time.Duration) []Candidate {
	cutoff := now.Add(-retention)
	var selected []Candidate
	for _, delivery := range deliveries {
		if delivery.DeliveryEndedAt.After(cutoff) || delivery.EducatorReportedAt.IsZero() {
			continue
		}
		for _, learner := range delivery.Learners {
			if !learner.Deadline.After(cutoff) {
				selected = append(selected, Candidate{
					CourseID: delivery.CourseID, LearnerID: learner.LearnerID, ObjectKey: learner.ObjectKey,
				})
			}
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ObjectKey < selected[j].ObjectKey })
	return selected
}

func LoadDeliveries(path string) ([]CourseDelivery, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read deliveries: %w", err)
	}
	var deliveries []CourseDelivery
	if err := json.Unmarshal(b, &deliveries); err != nil {
		return nil, fmt.Errorf("decode deliveries: %w", err)
	}
	return deliveries, nil
}
