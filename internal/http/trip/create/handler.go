package create

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	usecaseCreate "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
)

const maxBodyBytes = 1 << 20

type Handler struct {
	uc  createUsecase
	log *slog.Logger
}

type coordinatesRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type tripRequest struct {
	UserID     *uuid.UUID          `json:"user_id"`
	DriverID   *uuid.UUID          `json:"driver_id"`
	StartPoint *coordinatesRequest `json:"start_point"`
	EndPoint   *coordinatesRequest `json:"end_point"`
	Price      *int64              `json:"price"`
}

func New(uc createUsecase, log *slog.Logger) *Handler {
	return &Handler{uc: uc, log: log}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	in, err := decodeInput(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		domainerror.Write(w, r, h.log, err)
		return
	}

	t, repeated, err := h.uc.Do(r.Context(), in, params.IdempotencyKey)
	if err != nil {
		domainerror.Write(w, r, h.log, err)
		return
	}

	status := http.StatusCreated
	if repeated {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/api/v1/trips/"+t.ID.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(toAPI(t))
}

func decodeInput(body io.Reader) (usecaseCreate.Input, error) {
	dec := json.NewDecoder(body)
	// Запрещаем неизвестные поля и мусор после JSON
	dec.DisallowUnknownFields()

	var req tripRequest
	if err := dec.Decode(&req); err != nil {
		return usecaseCreate.Input{}, &domaintrip.ValidationError{Field: "body", Message: err.Error()}
	}
	// после первого объекта допустимо только конец тела
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return usecaseCreate.Input{}, &domaintrip.ValidationError{Field: "body", Message: "unexpected data after JSON object"}
	}
	return req.toInput()
}

func (req tripRequest) toInput() (usecaseCreate.Input, error) {
	if req.UserID == nil {
		return usecaseCreate.Input{}, required("user_id")
	}
	if req.DriverID == nil {
		return usecaseCreate.Input{}, required("driver_id")
	}
	startLat, startLon, err := req.StartPoint.require("start_point")
	if err != nil {
		return usecaseCreate.Input{}, err
	}
	endLat, endLon, err := req.EndPoint.require("end_point")
	if err != nil {
		return usecaseCreate.Input{}, err
	}
	if req.Price == nil {
		return usecaseCreate.Input{}, required("price")
	}
	return usecaseCreate.Input{
		UserID:         *req.UserID,
		DriverID:       *req.DriverID,
		StartLatitude:  startLat,
		StartLongitude: startLon,
		EndLatitude:    endLat,
		EndLongitude:   endLon,
		Price:          *req.Price,
	}, nil
}

func (c *coordinatesRequest) require(name string) (lat, lon float64, err error) {
	if c == nil {
		return 0, 0, required(name)
	}
	if c.Latitude == nil {
		return 0, 0, required(name + ".latitude")
	}
	if c.Longitude == nil {
		return 0, 0, required(name + ".longitude")
	}
	return *c.Latitude, *c.Longitude, nil
}

func required(field string) error {
	return &domaintrip.ValidationError{Field: field, Message: "required"}
}
