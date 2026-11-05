package domain

import "math"

// ICC2K estimates two-way random-effects, absolute-agreement reliability of the
// average of k raters. Rows are items, columns are panelists, in the same units.
// A complete matrix with at least two items and two panelists is required.
// See Shrout & Fleiss (1979), ICC(2,k), also documented by psych::ICC:
// https://www.personality-project.org/r/psych/help/ICC.html
func ICC2K(matrix [][]float64) *float64 {
	n := len(matrix)
	if n < 2 || len(matrix[0]) < 2 {
		return nil
	}
	k := len(matrix[0])
	rowMeans, columnMeans := make([]float64, n), make([]float64, k)
	grand := 0.0
	for i, row := range matrix {
		if len(row) != k {
			return nil
		}
		for j, value := range row {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil
			}
			rowMeans[i] += value / float64(k)
			columnMeans[j] += value / float64(n)
			grand += value / float64(n*k)
		}
	}
	ssRows, ssColumns, ssError := 0.0, 0.0, 0.0
	for i, row := range matrix {
		ssRows += float64(k) * math.Pow(rowMeans[i]-grand, 2)
		for j, value := range row {
			ssError += math.Pow(value-rowMeans[i]-columnMeans[j]+grand, 2)
		}
	}
	for _, mean := range columnMeans {
		ssColumns += float64(n) * math.Pow(mean-grand, 2)
	}
	msRows := ssRows / float64(n-1)
	msColumns := ssColumns / float64(k-1)
	msError := ssError / float64((n-1)*(k-1))
	denominator := msRows + (msColumns-msError)/float64(n)
	if denominator <= 1e-12*math.Max(1, math.Max(msRows, math.Max(msColumns, msError))) {
		return nil
	}
	result := (msRows - msError) / denominator
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return nil
	}
	return &result
}
