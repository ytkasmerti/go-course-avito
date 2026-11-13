//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package server

import (
	"context"
	"net/http"

	api "github.com/ytkasmerti/go-course-avito/internal/generated"
)

type (
	createHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams)
	}
	tripHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId)
	}
	pinger interface {
		Ping(ctx context.Context) error
	}
)
