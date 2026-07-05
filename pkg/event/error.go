package event

import "time"

type ProcessingError struct {
	PluginID  string    `json:"plugin_id"`
	Stage     string    `json:"stage"`
	ErrorCode string    `json:"error_code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
	Time      time.Time `json:"time"`
}
