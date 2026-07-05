package mvp

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	mysqlstore "xdp/pkg/storage/mysql"
)

func (h *Handler) listSavedSearches(w http.ResponseWriter, r *http.Request) {
	if h.mysql != nil {
		items, err := h.mysql.ListSavedSearches(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "list saved searches failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved_searches": items})
		return
	}

	h.mu.RLock()
	items := make([]mysqlstore.SavedSearch, 0, len(h.savedSearches))
	for _, item := range h.savedSearches {
		if item.Status == "" || item.Status == "active" {
			items = append(items, item)
		}
	}
	h.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"saved_searches": items})
}

func (h *Handler) createSavedSearch(w http.ResponseWriter, r *http.Request) {
	var req savedSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid saved search request")
		return
	}
	item, err := normalizeSavedSearchRequest(req)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	item.ID = newUUIDString()
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now

	if h.mysql != nil {
		saved, err := h.mysql.SaveSavedSearch(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadGateway, "save saved search failed")
			return
		}
		writeJSON(w, http.StatusCreated, saved)
		return
	}

	h.mu.Lock()
	h.savedSearches[item.ID] = item
	h.mu.Unlock()
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) deleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "saved search id is required")
		return
	}

	if h.mysql != nil {
		if err := h.mysql.DeleteSavedSearch(r.Context(), id); err != nil {
			if err == sql.ErrNoRows {
				writeErrorCode(w, http.StatusNotFound, "SAVED_SEARCH_NOT_FOUND", "saved search not found")
				return
			}
			writeError(w, http.StatusBadGateway, "delete saved search failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
		return
	}

	h.mu.Lock()
	item, ok := h.savedSearches[id]
	if ok && item.Status != "deleted" {
		item.Status = "deleted"
		item.UpdatedAt = time.Now()
		h.savedSearches[id] = item
	}
	h.mu.Unlock()
	if !ok {
		writeErrorCode(w, http.StatusNotFound, "SAVED_SEARCH_NOT_FOUND", "saved search not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

type savedSearchRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	SPL           string `json:"spl"`
	TimeRangeType string `json:"time_range_type"`
	Earliest      string `json:"earliest"`
	Latest        string `json:"latest"`
}

func normalizeSavedSearchRequest(req savedSearchRequest) (mysqlstore.SavedSearch, error) {
	spl := strings.TrimSpace(req.SPL)
	if spl == "" {
		return mysqlstore.SavedSearch{}, fmt.Errorf("spl is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = truncateRunes(spl, 80)
	}
	timeRangeType := strings.TrimSpace(req.TimeRangeType)
	if timeRangeType == "" {
		timeRangeType = "近 1 天"
	}
	return mysqlstore.SavedSearch{
		Name:          name,
		Description:   strings.TrimSpace(req.Description),
		SPL:           spl,
		TimeRangeType: timeRangeType,
		Earliest:      strings.TrimSpace(req.Earliest),
		Latest:        strings.TrimSpace(req.Latest),
		Visibility:    "private",
		Status:        "active",
	}, nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func newUUIDString() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
