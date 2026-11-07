package finish

import (
	"encoding/json"
	"log/slog"
	"net/http"

	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
)

type Handler struct {
	uc  finishUsecase
	log *slog.Logger
}

func New(uc finishUsecase, log *slog.Logger) *Handler {
	return &Handler{uc: uc, log: log}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	t, err := h.uc.Do(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, r, h.log, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toAPI(t))
}
