package ml

import (
	"fmt"
	"math"
)

// StandardScaler standardizes features by removing the mean and scaling to unit variance.
// Fitting MUST be performed strictly on training data.
type StandardScaler struct {
	Features []string  `json:"features"`
	Means    []float64 `json:"means"`
	Stds     []float64 `json:"stds"`
}

// NewStandardScaler creates an uninitialized scaler for the given feature list.
func NewStandardScaler(features []string) *StandardScaler {
	return &StandardScaler{
		Features: features,
		Means:    make([]float64, len(features)),
		Stds:     make([]float64, len(features)),
	}
}

// Fit computes the mean and standard deviation for each feature on the training matrix.
// X is an m x n slice (m samples, n features).
func (s *StandardScaler) Fit(X [][]float64) error {
	if len(X) == 0 {
		return fmt.Errorf("empty training dataset for scaler")
	}
	n := len(s.Features)
	m := float64(len(X))

	s.Means = make([]float64, n)
	s.Stds = make([]float64, n)

	// Compute means
	for _, row := range X {
		if len(row) != n {
			return fmt.Errorf("row length %d does not match feature count %d", len(row), n)
		}
		for j, val := range row {
			s.Means[j] += val
		}
	}
	for j := range s.Means {
		s.Means[j] /= m
	}

	// Compute sample standard deviations
	for _, row := range X {
		for j, val := range row {
			d := val - s.Means[j]
			s.Stds[j] += d * d
		}
	}
	for j := range s.Stds {
		variance := s.Stds[j] / m
		std := math.Sqrt(variance)
		if std < 1e-9 {
			std = 1.0 // constant feature: avoid division by zero
		}
		s.Stds[j] = std
	}

	return nil
}

// Transform standardizes a matrix using the fitted means and standard deviations.
func (s *StandardScaler) Transform(X [][]float64) [][]float64 {
	out := make([][]float64, len(X))
	n := len(s.Features)
	for i, row := range X {
		scaled := make([]float64, n)
		for j := 0; j < n; j++ {
			scaled[j] = (row[j] - s.Means[j]) / s.Stds[j]
		}
		out[i] = scaled
	}
	return out
}

// TransformRow standardizes a single feature vector.
func (s *StandardScaler) TransformRow(row []float64) []float64 {
	n := len(s.Features)
	scaled := make([]float64, n)
	for j := 0; j < n; j++ {
		scaled[j] = (row[j] - s.Means[j]) / s.Stds[j]
	}
	return scaled
}
