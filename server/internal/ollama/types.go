package ollama

import "time"

// Model represents a locally available Ollama model.
type Model struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Family     string    `json:"family"`
	Parameters string    `json:"parameter_size"`
	Quant      string    `json:"quantization_level"`
	ModifiedAt time.Time `json:"modified_at"`
}

// PullProgress represents a single progress event during model pull.
type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// tagsResponse is the JSON structure returned by GET /api/tags.
type tagsResponse struct {
	Models []tagsModel `json:"models"`
}

// tagsModel maps the raw Ollama API response for a single model.
type tagsModel struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Details    struct {
		Family            string `json:"family"`
		ParameterSize     string `json:"parameter_size"`
		QuantizationLevel string `json:"quantization_level"`
	} `json:"details"`
}
