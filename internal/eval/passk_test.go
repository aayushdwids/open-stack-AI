package eval

import (
	"math"
	"testing"
)

func TestPassAtK(t *testing.T) {
	cases := []struct {
		n, c, k int
		want    float64
	}{
		{1, 1, 1, 1.0},  // one sample, correct
		{1, 0, 1, 0.0},  // one sample, wrong
		{5, 0, 1, 0.0},  // none correct
		{5, 5, 1, 1.0},  // all correct
		{10, 5, 1, 0.5}, // half correct, pass@1
		{2, 1, 2, 1.0},  // n-c < k => 1
	}
	for _, tc := range cases {
		got := passAtK(tc.n, tc.c, tc.k)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("passAtK(%d,%d,%d)=%.4f want %.4f", tc.n, tc.c, tc.k, got, tc.want)
		}
	}
}

func TestBundledDatasetLoads(t *testing.T) {
	ds, err := LoadDataset("bundled:humanevalplus-mini")
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Cases) == 0 || ds.Digest() == "" {
		t.Fatalf("dataset empty or no digest: %+v", ds)
	}
	// Digest is deterministic for reproducibility.
	ds2, _ := LoadDataset("bundled:humanevalplus-mini")
	if ds.Digest() != ds2.Digest() {
		t.Error("dataset digest not stable")
	}
}
