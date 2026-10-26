package trip_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ytkasmerti/go-course-avito/internal/domain/trip"
)

func TestValidationError_Error(t *testing.T) {
	err := &trip.ValidationError{Field: "price", Message: "must be >= 0"}
	require.Equal(t, "price: must be >= 0", err.Error())
}
