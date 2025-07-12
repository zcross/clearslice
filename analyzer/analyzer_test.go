package clearslice

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestClearSliceAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NewAnalyzer(), "a")
}

func TestRecommendationPremise(t *testing.T) {
	s := []string{"foo", "bar", "baz"}
	linted := s[:0]
	recommended := slices.Delete(s, 0, len(s))
	require.Equal(t, linted, recommended)
	require.Len(t, recommended, len(linted))
}
