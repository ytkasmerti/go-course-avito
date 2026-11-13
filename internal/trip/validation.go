package trip

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

type coordinatesDTO struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type createTripDTO struct {
	UserID     *uuid.UUID      `json:"user_id"`
	DriverID   *uuid.UUID      `json:"driver_id"`
	StartPoint *coordinatesDTO `json:"start_point"`
	EndPoint   *coordinatesDTO `json:"end_point"`
	Price      *int64          `json:"price"`
}

func decodeCreateTrip(w http.ResponseWriter, r *http.Request) (CreateInput, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var dto createTripDTO
	if err := dec.Decode(&dto); err != nil {
		return CreateInput{}, fmt.Errorf("invalid JSON body: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return CreateInput{}, errors.New("body must contain exactly one JSON object")
	}
	return dto.validate()
}

func (d *createTripDTO) validate() (CreateInput, error) {
	var in CreateInput

	if d.UserID == nil {
		return in, errors.New("user_id is required")
	}
	if *d.UserID == uuid.Nil {
		return in, errors.New("user_id must not be a zero UUID")
	}
	if d.DriverID == nil {
		return in, errors.New("driver_id is required")
	}
	if *d.DriverID == uuid.Nil {
		return in, errors.New("driver_id must not be a zero UUID")
	}

	sLat, sLon, err := d.StartPoint.validate("start_point")
	if err != nil {
		return in, err
	}
	eLat, eLon, err := d.EndPoint.validate("end_point")
	if err != nil {
		return in, err
	}

	if d.Price == nil {
		return in, errors.New("price is required")
	}
	if *d.Price < 0 {
		return in, errors.New("price must be non-negative")
	}

	return CreateInput{
		UserID:         *d.UserID,
		DriverID:       *d.DriverID,
		StartLatitude:  sLat,
		StartLongitude: sLon,
		EndLatitude:    eLat,
		EndLongitude:   eLon,
		Price:          *d.Price,
	}, nil
}

func (c *coordinatesDTO) validate(name string) (lat, lon float64, err error) {
	if c == nil {
		return 0, 0, fmt.Errorf("%s is required", name)
	}
	if c.Latitude == nil {
		return 0, 0, fmt.Errorf("%s.latitude is required", name)
	}
	if c.Longitude == nil {
		return 0, 0, fmt.Errorf("%s.longitude is required", name)
	}
	if *c.Latitude < -90 || *c.Latitude > 90 {
		return 0, 0, fmt.Errorf("%s.latitude must be in [-90, 90]", name)
	}
	if *c.Longitude < -180 || *c.Longitude > 180 {
		return 0, 0, fmt.Errorf("%s.longitude must be in [-180, 180]", name)
	}
	return *c.Latitude, *c.Longitude, nil
}
