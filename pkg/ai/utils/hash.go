package utils

import "strconv"

func ShortHash(value string) string {
	h1 := uint32(0xdeadbeef)
	h2 := uint32(0x41c6ce57)
	for _, ch := range utf16CodeUnits(value) {
		h1 = imul(h1^uint32(ch), 2654435761)
		h2 = imul(h2^uint32(ch), 1597334677)
	}
	h1 = imul(h1^(h1>>16), 2246822507) ^ imul(h2^(h2>>13), 3266489909)
	h2 = imul(h2^(h2>>16), 2246822507) ^ imul(h1^(h1>>13), 3266489909)
	return strconv.FormatUint(uint64(h2), 36) + strconv.FormatUint(uint64(h1), 36)
}

func imul(a uint32, b uint32) uint32 {
	return uint32(uint64(a) * uint64(b))
}

func utf16CodeUnits(value string) []uint16 {
	units := []uint16{}
	for _, r := range value {
		if r <= 0xffff {
			units = append(units, uint16(r))
			continue
		}
		r -= 0x10000
		units = append(units, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&0x3ff)))
	}
	return units
}
