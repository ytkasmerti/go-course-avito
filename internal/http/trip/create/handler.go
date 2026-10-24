package create

import (
	"encoding/json"
	"net/http"

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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		status, p := domainerror.Problem(&validationError{field: "body", msg: "invalid json"})
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

type validationError struct {
	field string
	msg   string
}

func (e *validationError) Error() string {
	return e.field + ": " + e.msg
}
