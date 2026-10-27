package create

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	usecaseCreate "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
)

type Handler struct {
	uc createUsecase
}

func New(uc createUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	var body api.CreateTripJSONRequestBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "body", Message: err.Error()})
		writeProblem(w, status, p)
		return
	}

	if body.UserId == uuid.Nil {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "user_id", Message: "required"})
		writeProblem(w, status, p)
		return
	}
	if body.DriverId == uuid.Nil {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "driver_id", Message: "required"})
		writeProblem(w, status, p)
		return
	}
	if body.Price < 0 {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "price", Message: "must be >= 0"})
		writeProblem(w, status, p)
		return
	}
	if body.StartPoint.Latitude < -90 || body.StartPoint.Latitude > 90 {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "start_point.latitude", Message: "out of range"})
		writeProblem(w, status, p)
		return
	}
	if body.EndPoint.Latitude < -90 || body.EndPoint.Latitude > 90 {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "end_point.latitude", Message: "out of range"})
		writeProblem(w, status, p)
		return
	}
	if body.StartPoint.Longitude < -180 || body.StartPoint.Longitude > 180 {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "start_point.longitude", Message: "out of range"})
		writeProblem(w, status, p)
		return
	}
	if body.EndPoint.Longitude < -180 || body.EndPoint.Longitude > 180 {
		status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "end_point.longitude", Message: "out of range"})
		writeProblem(w, status, p)
		return
	}

	in := usecaseCreate.Input{
		UserID:         body.UserId,
		DriverID:       body.DriverId,
		StartLatitude:  body.StartPoint.Latitude,
		StartLongitude: body.StartPoint.Longitude,
		EndLatitude:    body.EndPoint.Latitude,
		EndLongitude:   body.EndPoint.Longitude,
		Price:          body.Price,
	}

	t, repeated, err := h.uc.Do(r.Context(), in, params.IdempotencyKey)
	if err != nil {
		status, p := domainerror.Problem(err)
		writeProblem(w, status, p)
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

func writeProblem(w http.ResponseWriter, status int, p api.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
