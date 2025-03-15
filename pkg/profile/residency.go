package profile

// buildResidencyTable returns a map of residency fractions per stack trace.
func (p *Profiler) buildResidencyTable(histogram map[string]int, sampleCount int) map[string]float64 {
	residencyTable := make(map[string]float64, len(histogram))
	for trace, count := range histogram {
		residency := float64(count) / float64(sampleCount)
		residencyTable[trace] = residency
	}

	return residencyTable
}
