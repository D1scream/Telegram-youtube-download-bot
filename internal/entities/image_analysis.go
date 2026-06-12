package entities

type ImageAnalysis struct {
	Description     string `json:"description"`
	SearchQuery     string `json:"search_query"`
	OffenseDetected bool   `json:"offense_detected"`
	Error           string `json:"error"`
}
