package api

import (
	"log/slog"
	"net/http"

	"xdp/services/api/internal/mvp"
)

func NewHandler(logger *slog.Logger) http.Handler {
	return mvp.NewHandler(logger)
}
