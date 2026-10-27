package osmdata

import "math"

func ringArea(p []Point) float64 {
	var s float64
	for i := 1; i < len(p); i++ {
		s += p[i-1][0]*p[i][1] - p[i][0]*p[i-1][1]
	}
	return math.Abs(s) / 2
}
func ringCentroid(p []Point) Point {
	var x, y, a float64
	for i := 1; i < len(p); i++ {
		c := p[i-1][0]*p[i][1] - p[i][0]*p[i-1][1]
		a += c
		x += (p[i-1][0] + p[i][0]) * c
		y += (p[i-1][1] + p[i][1]) * c
	}
	if math.Abs(a) < 1e-12 {
		return p[0]
	}
	return Point{x / (3 * a), y / (3 * a)}
}
func lineMidpoint(p []Point) Point {
	var total float64
	for i := 1; i < len(p); i++ {
		total += math.Hypot(p[i][0]-p[i-1][0], p[i][1]-p[i-1][1])
	}
	left := total / 2
	for i := 1; i < len(p); i++ {
		d := math.Hypot(p[i][0]-p[i-1][0], p[i][1]-p[i-1][1])
		if d > 0 && left <= d {
			return Point{p[i-1][0] + (p[i][0]-p[i-1][0])*left/d, p[i-1][1] + (p[i][1]-p[i-1][1])*left/d}
		}
		left -= d
	}
	return p[0]
}
func ToCentroid(f Feature) Feature {
	var p Point
	switch c := f.Geometry.Coordinates.(type) {
	case Point:
		return f
	case []Point:
		p = lineMidpoint(c)
	case [][]Point:
		if f.Geometry.Type == "Polygon" {
			p = ringCentroid(c[0])
		} else {
			longest := c[0]
			for _, line := range c {
				if len(line) > len(longest) {
					longest = line
				}
			}
			p = lineMidpoint(longest)
		}
	case [][][]Point:
		largest := c[0][0]
		for _, poly := range c {
			if ringArea(poly[0]) > ringArea(largest) {
				largest = poly[0]
			}
		}
		p = ringCentroid(largest)
	}
	f.Properties["original_geometry"] = f.Geometry.Type
	f.Geometry = Geometry{"Point", p}
	return f
}
