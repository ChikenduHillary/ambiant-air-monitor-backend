package aqi

import "math"

type Breakpoint struct {
	CLo, CHi   float64
	ILo, IHi   int
	Label, Color string
}

var Breakpoints = []Breakpoint{
	{0.0, 12.0, 0, 50, "Good", "#34d399"},
	{12.1, 35.4, 51, 100, "Moderate", "#f59e0b"},
	{35.5, 55.4, 101, 150, "Unhealthy for Sensitive Groups", "#f97316"},
	{55.5, 150.4, 151, 200, "Unhealthy", "#ef4444"},
	{150.5, 250.4, 201, 300, "Very Unhealthy", "#9b1c1c"},
}

// FromPM25 calculates AQI from PM2.5 concentration using EPA breakpoints.
func FromPM25(pm25 float64) int {
	for _, b := range Breakpoints {
		if pm25 >= b.CLo && pm25 <= b.CHi {
			return int(math.Round(float64(b.IHi-b.ILo)/(b.CHi-b.CLo)*(pm25-b.CLo) + float64(b.ILo)))
		}
	}
	if pm25 > 250.4 {
		return 301
	}
	return 0
}

// Category returns a human-readable label and hex color for a given AQI value.
func Category(aqiVal int) (label, color string) {
	for _, b := range Breakpoints {
		if aqiVal >= b.ILo && aqiVal <= b.IHi {
			return b.Label, b.Color
		}
	}
	return "Hazardous", "#7e1d1d"
}
