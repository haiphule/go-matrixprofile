package matrixprofile

import (
	"fmt"
	"gonum.org/v1/gonum/fourier"
	"gonum.org/v1/gonum/stat"
	"math"
	"math/rand"
)

// zNormalize computes a z-normalized version of a slice of floats. This is represented by y[i] = x[i] - mean(x)/std(x)
func zNormalize(ts []float64) ([]float64, error) {
	var i int

	if len(ts) == 0 {
		return nil, fmt.Errorf("slice does not have any data")
	}

	m := stat.Mean(ts, nil)

	out := make([]float64, len(ts))
	for i = 0; i < len(ts); i++ {
		out[i] = ts[i] - m
	}

	var std float64
	for _, val := range out {
		std += val * val
	}
	std = math.Sqrt(std / float64(len(out)))

	if std == 0 {
		return out, fmt.Errorf("standard deviation is zero")
	}

	for i = 0; i < len(ts); i++ {
		out[i] = out[i] / std
	}

	return out, nil
}

// movstd computes the standard deviation of each sliding window of m over a slice of floats. This is done by one pass through the data and keeping track of the cumulative sum and cumulative sum squared. Diffs between these at intervals of m provide a total of O(n) calculations for the standard deviation of each window of size m for the time series ts.
func movstd(ts []float64, m int) ([]float64, error) {
	if m <= 1 {
		return nil, fmt.Errorf("length of slice must be greater than 1")
	}

	if m >= len(ts) {
		return nil, fmt.Errorf("m must be less than length of slice")
	}

	var i int

	c := make([]float64, len(ts)+1)
	csqr := make([]float64, len(ts)+1)
	for i = 0; i < len(ts)+1; i++ {
		if i == 0 {
			c[i] = 0
			csqr[i] = 0
		} else {
			c[i] = ts[i-1] + c[i-1]
			csqr[i] = ts[i-1]*ts[i-1] + csqr[i-1]
		}
	}

	out := make([]float64, len(ts)-m+1)
	for i = 0; i < len(ts)-m+1; i++ {
		out[i] = math.Sqrt((csqr[i+m]-csqr[i])/float64(m) - (c[i+m]-c[i])*(c[i+m]-c[i])/float64(m*m))
	}

	return out, nil
}

// slidingDotProductV2 computes the sliding dot product between two slices given a query and time series. Uses fast fourier transforms to compute the necessary values
func slidingDotProduct(q, t []float64) ([]float64, error) {
	m := len(q)
	n := len(t)

	if m*2 >= n {
		return nil, fmt.Errorf("length of query must be less than half the timeseries")
	}

	if m < 2 {
		return nil, fmt.Errorf("query must be at least length 2")
	}

	qpad := make([]float64, len(t))
	for i := 0; i < len(q); i++ {
		qpad[i] = q[m-i-1]
	}

	fft := fourier.NewFFT(len(t))
	tf := fft.Coefficients(nil, t)
	qf := fft.Coefficients(nil, qpad)

	f, err := multComplexSlice(tf, qf)
	if err != nil {
		return nil, err
	}
	dot := fft.Sequence(nil, f)

	out := make([]float64, n-m+1)
	for i := 0; i < len(out); i++ {
		out[i] = dot[m-1+i] / float64(len(t))
	}
	return out, nil
}

func multComplexSlice(a, b []complex128) ([]complex128, error) {
	if len(a) != len(b) {
		return nil, fmt.Errorf("length of both complex slices are not the same")
	}

	out := make([]complex128, len(a))
	for i := 0; i < len(a); i++ {
		out[i] = a[i] * b[i]
	}
	return out, nil
}

// Mass calculates the Mueen's algorithm for similarity search (MASS) between a specified query and timeseries.
func Mass(q, t []float64) ([]float64, error) {
	m := len(q)
	n := len(t)

	if m < 2 {
		return nil, fmt.Errorf("need at least 2 samples for the query")
	}

	if m*2 >= n {
		return nil, fmt.Errorf("query must be less than half of the timeseries")
	}

	qnorm, err := zNormalize(q)
	if err != nil {
		return nil, err
	}

	std, err := movstd(t, m)
	if err != nil {
		return nil, err
	}

	dot, err := slidingDotProduct(qnorm, t)
	if err != nil {
		return nil, err
	}

	if len(std) != len(dot) {
		return nil, fmt.Errorf("length of rolling standard deviation, %d, is not the same as the sliding dot product, %d", len(std), len(dot))
	}

	// converting cross correlation value to euclidian distance
	out := make([]float64, len(dot))
	for i := 0; i < len(dot); i++ {
		out[i] = math.Sqrt(math.Abs(2 * (float64(m) - (dot[i] / std[i]))))
	}
	return out, nil
}

// distanceProfile computes the distance profile between a and b time series. If b is set to nil then it assumes a self join and will create an exclusion area for trivial nearest neighbors
func distanceProfile(a, b []float64, m, idx int) ([]float64, error) {

	var selfJoin bool
	if b == nil {
		selfJoin = true
		b = a
	}

	if idx+m > len(a) {
		return nil, fmt.Errorf("index %d with m %d asks for data beyond the length of a, %d", idx, m, len(a))
	}

	query := a[idx : idx+m]
	profile, err := Mass(query, b)
	if err != nil {
		return nil, err
	}
	if selfJoin {
		startIdx := 0
		if idx-m/2 > startIdx {
			startIdx = idx - m/2
		}
		endIdx := len(profile)
		if idx+m/2 < endIdx {
			endIdx = idx + m/2
		}
		for i := startIdx; i < endIdx; i++ {
			profile[i] = math.Inf(1)
		}
	}
	return profile, nil
}

// Stmp computes the full matrix profile given two time series as inputs. If the second time series is set to nil then a self join on the first will be performed.
func Stmp(a, b []float64, m int) ([]float64, []int, error) {
	if a == nil || len(a) == 0 {
		return nil, nil, fmt.Errorf("first slice is nil or has a length of 0")
	}

	if b != nil && len(b) == 0 {
		return nil, nil, fmt.Errorf("second slice must be nil for self-join operation or have a length greater than 0")
	}

	n := len(b)
	var mp []float64
	var mpIdx []int
	if b == nil {
		mp = make([]float64, len(a)-m+1)
		mpIdx = make([]int, len(a)-m+1)
		n = len(a)
	} else {
		mp = make([]float64, len(b)-m+1)
		mpIdx = make([]int, len(b)-m+1)
	}

	for i := 0; i < len(mp); i++ {
		mp[i] = math.Inf(1)
		mpIdx[i] = math.MaxInt64
	}

	var profile []float64
	var err error
	var i, j int
	for i = 0; i < n-m+1; i++ {
		profile, err = distanceProfile(a, b, m, i)
		if err != nil {
			return nil, nil, err
		}
		if len(profile) != len(mp) {
			return nil, nil, fmt.Errorf("distance profile length, %d, and initialized matrix profile length, %d, do not match", len(profile), len(mp))
		}
		for j = 0; j < len(profile); j++ {
			if profile[j] <= mp[j] {
				mp[j] = profile[j]
				mpIdx[j] = i
			}
		}
	}
	return mp, mpIdx, nil
}

// Stamp uses random ordering to compute the matrix profile. User can the sample to anything between 0 and 1 so that the computation early terminates and provides the current computed matrix profile. This should compute far faster at the cost of an approximation of the matrix profile
func Stamp(a, b []float64, m int, sample float64) ([]float64, []int, error) {
	if sample == 0.0 {
		return nil, nil, fmt.Errorf("must provide a non zero sampling")
	}
	if a == nil || len(a) == 0 {
		return nil, nil, fmt.Errorf("first slice is nil or has a length of 0")
	}

	if b != nil && len(b) == 0 {
		return nil, nil, fmt.Errorf("second slice must be nil for self-join operation or have a length greater than 0")
	}

	n := len(b)
	var mp []float64
	var mpIdx []int
	if b == nil {
		mp = make([]float64, len(a)-m+1)
		mpIdx = make([]int, len(a)-m+1)
		n = len(a)
	} else {
		mp = make([]float64, len(b)-m+1)
		mpIdx = make([]int, len(b)-m+1)
	}

	for i := 0; i < len(mp); i++ {
		mp[i] = math.Inf(1)
		mpIdx[i] = math.MaxInt64
	}

	var profile []float64
	var err error
	var i, j int

	randIdx := rand.Perm(n - m + 1)
	for i = 0; i < int(float64(n-m+1)*sample); i++ {
		profile, err = distanceProfile(a, b, m, randIdx[i])
		if err != nil {
			return nil, nil, err
		}
		if len(profile) != len(mp) {
			return nil, nil, fmt.Errorf("distance profile length, %d, and initialized matrix profile length, %d, do not match", len(profile), len(mp))
		}
		for j = 0; j < len(profile); j++ {
			if profile[j] <= mp[j] {
				mp[j] = profile[j]
				mpIdx[j] = randIdx[i]
			}
		}
	}
	return mp, mpIdx, nil
}
