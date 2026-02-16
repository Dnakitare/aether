// Package api provides pagination support for list endpoints.
package api

import (
	"net/http"
	"strconv"
)

// PaginationParams represents pagination query parameters.
type PaginationParams struct {
	Page     int `json:"page"`      // Current page number (1-based)
	PageSize int `json:"page_size"` // Number of items per page
	Offset   int `json:"-"`         // Calculated offset for database queries
}

// PaginationMeta represents pagination metadata in responses.
type PaginationMeta struct {
	Page       int  `json:"page"`        // Current page number
	PageSize   int  `json:"page_size"`   // Items per page
	TotalItems int  `json:"total_items"` // Total number of items
	TotalPages int  `json:"total_pages"` // Total number of pages
	HasNext    bool `json:"has_next"`    // Whether there's a next page
	HasPrev    bool `json:"has_prev"`    // Whether there's a previous page
}

// PaginatedResponse wraps a list response with pagination metadata.
type PaginatedResponse struct {
	Data       interface{}     `json:"data"`
	Pagination *PaginationMeta `json:"pagination"`
}

// Default pagination values
const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// parsePaginationParams extracts and validates pagination parameters from request.
func parsePaginationParams(r *http.Request) PaginationParams {
	params := PaginationParams{
		Page:     DefaultPage,
		PageSize: DefaultPageSize,
	}

	// Parse page parameter
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			params.Page = page
		}
	}

	// Parse page_size parameter
	if pageSizeStr := r.URL.Query().Get("page_size"); pageSizeStr != "" {
		if pageSize, err := strconv.Atoi(pageSizeStr); err == nil && pageSize > 0 {
			params.PageSize = pageSize
			// Enforce maximum page size
			if params.PageSize > MaxPageSize {
				params.PageSize = MaxPageSize
			}
		}
	}

	// Calculate offset for database queries
	params.Offset = (params.Page - 1) * params.PageSize

	return params
}

// buildPaginationMeta creates pagination metadata for response.
func buildPaginationMeta(params PaginationParams, totalItems int) *PaginationMeta {
	totalPages := (totalItems + params.PageSize - 1) / params.PageSize
	if totalPages < 1 {
		totalPages = 1
	}

	return &PaginationMeta{
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasNext:    params.Page < totalPages,
		HasPrev:    params.Page > 1,
	}
}

// respondPaginated sends a paginated JSON response.
func (s *Server) respondPaginated(w http.ResponseWriter, data interface{}, params PaginationParams, totalItems int) {
	response := PaginatedResponse{
		Data:       data,
		Pagination: buildPaginationMeta(params, totalItems),
	}
	s.respondJSON(w, http.StatusOK, response)
}
