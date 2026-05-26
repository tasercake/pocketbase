package apis

import (
	"strconv"

	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func availableThumbSizes(fieldThumbs []string) []string {
	available := make([]string, 0, len(defaultThumbSizes)+len(fieldThumbs))
	available = append(available, defaultThumbSizes...)
	available = append(available, fieldThumbs...)
	return available
}

type parsedThumbSize struct {
	value  string
	width  int
	height int
	suffix string
}

func selectThumbSize(collectionName string, requested string, available []string) (string, bool) {
	if requested == "" {
		return "", false
	}

	for _, size := range available {
		if size == requested {
			return requested, true
		}
	}

	if collectionName != "photos" {
		return "", false
	}

	req, ok := parseThumbSize(requested)
	if !ok {
		return "", false
	}

	candidates := make([]parsedThumbSize, 0, len(available))
	for _, size := range available {
		candidate, ok := parseThumbSize(size)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}

	compatible := candidates[:0]
	for _, candidate := range candidates {
		if candidate.suffix == req.suffix {
			compatible = append(compatible, candidate)
		}
	}
	if len(compatible) == 0 {
		compatible = candidates
	}

	best, found := closestLargerThumbSize(req, compatible)
	if found {
		return best.value, true
	}

	return largestThumbSize(req, compatible).value, true
}

func parseThumbSize(size string) (parsedThumbSize, bool) {
	matches := filesystem.ThumbSizeRegex.FindStringSubmatch(size)
	if len(matches) != 4 {
		return parsedThumbSize{}, false
	}
	width, err := strconv.Atoi(matches[1])
	if err != nil {
		return parsedThumbSize{}, false
	}
	height, err := strconv.Atoi(matches[2])
	if err != nil {
		return parsedThumbSize{}, false
	}
	if width == 0 && height == 0 {
		return parsedThumbSize{}, false
	}
	return parsedThumbSize{value: size, width: width, height: height, suffix: matches[3]}, true
}

func closestLargerThumbSize(requested parsedThumbSize, candidates []parsedThumbSize) (parsedThumbSize, bool) {
	var best parsedThumbSize
	found := false
	for _, candidate := range candidates {
		if !thumbAtLeast(requested, candidate) {
			continue
		}
		if !found || compareClosestLarger(requested, candidate, best) < 0 {
			best = candidate
			found = true
		}
	}
	return best, found
}

func thumbAtLeast(requested parsedThumbSize, candidate parsedThumbSize) bool {
	switch {
	case requested.height == 0:
		return candidate.width >= requested.width
	case requested.width == 0:
		return candidate.height >= requested.height
	default:
		return candidate.width >= requested.width && candidate.height >= requested.height
	}
}

func compareClosestLarger(requested parsedThumbSize, a parsedThumbSize, b parsedThumbSize) int {
	switch {
	case requested.height == 0:
		if d := (a.width - requested.width) - (b.width - requested.width); d != 0 {
			return d
		}
		if d := thumbArea(a) - thumbArea(b); d != 0 {
			return d
		}
	case requested.width == 0:
		if d := (a.height - requested.height) - (b.height - requested.height); d != 0 {
			return d
		}
		if d := thumbArea(a) - thumbArea(b); d != 0 {
			return d
		}
	default:
		if d := (thumbArea(a) - thumbArea(requested)) - (thumbArea(b) - thumbArea(requested)); d != 0 {
			return d
		}
		if d := (a.width - requested.width) - (b.width - requested.width); d != 0 {
			return d
		}
		if d := (a.height - requested.height) - (b.height - requested.height); d != 0 {
			return d
		}
	}
	if a.value < b.value {
		return -1
	}
	if a.value > b.value {
		return 1
	}
	return 0
}

func largestThumbSize(requested parsedThumbSize, candidates []parsedThumbSize) parsedThumbSize {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if compareLargest(requested, candidate, best) > 0 {
			best = candidate
		}
	}
	return best
}

func compareLargest(requested parsedThumbSize, a parsedThumbSize, b parsedThumbSize) int {
	switch {
	case requested.height == 0:
		if d := a.width - b.width; d != 0 {
			return d
		}
		if d := thumbArea(a) - thumbArea(b); d != 0 {
			return d
		}
	case requested.width == 0:
		if d := a.height - b.height; d != 0 {
			return d
		}
		if d := thumbArea(a) - thumbArea(b); d != 0 {
			return d
		}
	default:
		if d := thumbArea(a) - thumbArea(b); d != 0 {
			return d
		}
		if d := a.width - b.width; d != 0 {
			return d
		}
		if d := a.height - b.height; d != 0 {
			return d
		}
	}
	if a.value < b.value {
		return -1
	}
	if a.value > b.value {
		return 1
	}
	return 0
}

func thumbArea(size parsedThumbSize) int {
	return size.width * size.height
}
