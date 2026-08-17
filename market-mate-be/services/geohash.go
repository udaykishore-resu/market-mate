package services

// Geohash keys the store cache.
//
// The previous key rounded latitude and longitude to two decimals, which makes
// the cell width depend on latitude and, worse, splits neighbours that sit on
// either side of a rounding boundary — 37.7749 and 37.7751 are 22 metres apart
// and never share an entry. A geohash cell has the same failure at its own
// boundaries, but the cell is a stable published grid: precision 5 is roughly
// 4.9km x 4.9km, which is the radius the store search already uses, so one
// lookup genuinely serves everyone in the cell.
//
// Implemented here rather than pulled in: it is the standard interleave and the
// dependency would be larger than the code.

// geohashAlphabet is the base32 alphabet from Niemeyer's original spec: digits
// plus lowercase letters with a, i, l and o removed.
const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

// GeohashPrecision is the default cell size for cache keys: 5 characters,
// ~4.9km on the equator.
const GeohashPrecision = 5

// Geohash encodes a coordinate to the given number of base32 characters.
//
// Out-of-range coordinates are clamped rather than rejected: a geo-IP provider
// occasionally returns a latitude of 90.0000001, and refusing to build a cache
// key is a worse answer than binning it at the pole.
func Geohash(lat, lng float64, precision int) string {
	if precision <= 0 {
		return ""
	}
	lat = clamp(lat, -90, 90)
	lng = clamp(lng, -180, 180)

	latRange := [2]float64{-90, 90}
	lngRange := [2]float64{-180, 180}

	out := make([]byte, 0, precision)
	var bit, ch int
	evenBit := true // even bits halve longitude, odd bits halve latitude

	for len(out) < precision {
		if evenBit {
			mid := (lngRange[0] + lngRange[1]) / 2
			if lng >= mid {
				ch = ch<<1 | 1
				lngRange[0] = mid
			} else {
				ch <<= 1
				lngRange[1] = mid
			}
		} else {
			mid := (latRange[0] + latRange[1]) / 2
			if lat >= mid {
				ch = ch<<1 | 1
				latRange[0] = mid
			} else {
				ch <<= 1
				latRange[1] = mid
			}
		}
		evenBit = !evenBit

		if bit++; bit == 5 {
			out = append(out, geohashAlphabet[ch])
			bit, ch = 0, 0
		}
	}
	return string(out)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
