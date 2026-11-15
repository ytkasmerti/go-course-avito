//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package position

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	usecasePosition "github.com/ytkasmerti/go-course-avito/internal/usecase/position"
)

type (
	saveUsecase interface {
		Do(ctx context.Context, in usecasePosition.SaveInput) (*model.Position, error)
	}
	listUsecase interface {
		Do(ctx context.Context, tripID uuid.UUID) ([]model.Position, error)
	}
)

const (
	maxBodyBytes    = 1 << 20
	contentTypeJSON = "application/json"
)

type SaveHandler struct {
	uc  saveUsecase
	log *slog.Logger
}

func NewSave(uc saveUsecase, log *slog.Logger) *SaveHandler {
	return &SaveHandler{uc: uc, log: log}
}

type coordinatesRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type positionRequest struct {
	Coordinates *coordinatesRequest `json:"coordinates"`
	RecordedAt  *time.Time          `json:"recorded_at"`
}

func (h *SaveHandler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	in, err := decodeInput(http.MaxBytesReader(w, r.Body, maxBodyBytes), tripID)
	if err != nil {
		domainerror.Write(w, r, h.log, err)
		return
	}
	p, err := h.uc.Do(r.Context(), in)
	if err != nil {
		domainerror.Write(w, r, h.log, err)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toAPI(*p))
}

func decodeInput(body io.Reader, tripID api.TripId) (usecasePosition.SaveInput, error) {
	dec := json.NewDecoder(body)
	// Запрещаем неизвестные поля и мусор после JSON
	dec.DisallowUnknownFields()
	var req positionRequest
	if err := dec.Decode(&req); err != nil {
		return usecasePosition.SaveInput{}, &domaintrip.ValidationError{Field: "body", Message: err.Error()}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return usecasePosition.SaveInput{}, &domaintrip.ValidationError{Field: "body", Message: "unexpected data after JSON object"}
	}
	if req.Coordinates == nil {
		return usecasePosition.SaveInput{}, required("coordinates")
	}
	if req.Coordinates.Latitude == nil {
		return usecasePosition.SaveInput{}, required("coordinates.latitude")
	}
	if req.Coordinates.Longitude == nil {
		return usecasePosition.SaveInput{}, required("coordinates.longitude")
	}
	if req.RecordedAt == nil {
		return usecasePosition.SaveInput{}, required("recorded_at")
	}
	return usecasePosition.SaveInput{
		TripID:     tripID,
		Latitude:   *req.Coordinates.Latitude,
		Longitude:  *req.Coordinates.Longitude,
		RecordedAt: *req.RecordedAt,
	}, nil
}

func required(field string) error {
	return &domaintrip.ValidationError{Field: field, Message: "required"}
}

type ListHandler struct {
	uc  listUsecase
	log *slog.Logger
}

func NewList(uc listUsecase, log *slog.Logger) *ListHandler {
	return &ListHandler{uc: uc, log: log}
}

func (h *ListHandler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	list, err := h.uc.Do(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, r, h.log, err)
		return
	}
	positions := make([]api.TripPosition, 0, len(list))
	for _, p := range list {
		positions = append(positions, toAPI(p))
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(api.TripPositionsResponse{Positions: positions})
}

func toAPI(p model.Position) api.TripPosition {
	return api.TripPosition{
		Id:          p.ID,
		TripId:      p.TripID,
		Coordinates: api.Coordinates{Latitude: p.Latitude, Longitude: p.Longitude},
		RecordedAt:  p.RecordedAt,
	}
}
