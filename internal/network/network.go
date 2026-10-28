// Package network models the communication layer between blockchain nodes.
// It provides realistic delay sampling using a Gamma distribution.
package network

import (
	"math"
	"math/rand/v2"
)

// DelayModel holds parameters for sampling one-way transmission delays.
// The delay is modelled as Gamma(shape k, scale θ) where:
//
//	mean = k * θ  (= MeanMs / 1000 in seconds)
//	var  = k * θ²
//
// A larger k makes the distribution more "bell-shaped" (less variable).
// We default to k=4 which gives a coefficient of variation of 0.5,
// matching real-world network measurements reasonably well.
type DelayModel struct {
	// MeanSec is the mean delay in simulated seconds.
	MeanSec float64

	// Shape (k) controls how variable the delay is.
	// k=1 → exponential (high variance); k→∞ → deterministic.
	Shape float64
}

// NewDelayModel creates a DelayModel from millisecond parameters.
// meanMs and jitterMs come from the scenario config.
// jitterMs is used to infer the Gamma shape: shape = (meanMs/jitterMs)^2.
// A jitter of 0 is replaced with a tiny value to avoid divide-by-zero.
func NewDelayModel(meanMs, jitterMs float64) DelayModel {
	if jitterMs <= 0 {
		jitterMs = meanMs * 0.01 // near-zero variance → essentially deterministic
	}
	// Gamma parameterisation: shape k = (mean/std)^2
	shape := (meanMs / jitterMs) * (meanMs / jitterMs)
	if shape < 0.5 {
		shape = 0.5 // clamp: Gamma needs shape > 0
	}
	return DelayModel{
		MeanSec: meanMs / 1000.0,
		Shape:   shape,
	}
}

// Sample draws one delay value (in seconds) from the Gamma distribution.
// rng must be seeded externally to ensure reproducibility.
//
// Implementation: Marsaglia & Tsang (2000) "A Simple Method for Generating
// Gamma Variables". This is the standard approach used in gonum/stat/distuv.
// We inline it here to keep the M1 code self-contained.
func (d DelayModel) Sample(rng *rand.Rand) float64 {
	if d.MeanSec <= 0 {
		return 0
	}
	scale := d.MeanSec / d.Shape // θ = mean / shape

	// Marsaglia-Tsang: generate Gamma(shape, 1) then scale.
	g := gammaStd(rng, d.Shape)
	delay := g * scale

	// Floor at 1 microsecond to avoid zero delays corrupting event ordering.
	if delay < 1e-6 {
		delay = 1e-6
	}
	return delay
}

// gammaStd samples from Gamma(shape, scale=1) using Marsaglia-Tsang (2000).
// Handles shape < 1 via the Ahrens-Dieter trick (boost by Uniform^(1/shape)).
func gammaStd(rng *rand.Rand, shape float64) float64 {
	if shape < 1 {
		// Boost: if X ~ Gamma(shape+1,1), then X * U^(1/shape) ~ Gamma(shape,1)
		return gammaStd(rng, shape+1) * math.Pow(rng.Float64(), 1.0/shape)
	}

	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)

	for {
		var x, v float64
		for {
			x = rng.NormFloat64()
			v = 1 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v // v = (1 + c*x)^3
		u := rng.Float64()

		// Squeeze test (fast path).
		if u < 1-0.0331*(x*x)*(x*x) {
			return d * v
		}
		// Log test (slow path — rarely triggered).
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
