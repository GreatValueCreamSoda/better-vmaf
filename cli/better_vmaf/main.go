package main

import (
	"fmt"
	"math"
	"os"

	"github.com/GreatValueCreamSoda/better-vmaf/src"
	"github.com/spf13/pflag"
)

var (
	reference, distortion string
	vmafSubsampling       int
	compareChroma         bool
	compareChromaNeg      bool
	chromaWeight          int
	vmafMotion            bool
)

func init() {
	pflag.StringVarP(&reference, "reference", "r", "", "reference video file for VMAF comparison")
	pflag.StringVarP(&distortion, "distortion", "d", "", "distorted video file for VMAF comparison")
	pflag.IntVar(&vmafSubsampling, "vmaf-subsampling", 1, "calculate every X frame for faster comparisons")
	pflag.BoolVar(&compareChromaNeg, "no-compare-chroma", false, "disable chroma channels in VMAF scoring")
	pflag.IntVar(&chromaWeight, "chroma-weight", 2, "relative weight of chroma to luma channels (1/chroma-weight)")
	pflag.BoolVar(&vmafMotion, "vmaf-motion", false, "enable temporal VMAF scoring (not recommended for high-quality targets)")
	pflag.CommandLine.SortFlags = false
	pflag.Parse()

	compareChroma = !compareChromaNeg
}

func main() {
	vmaf := src.BetterVmaf{
		VC: src.VmafComputer{
			ReferencePath:  reference,
			DistortionPath: distortion,
			CompareChroma:  compareChroma,
			VmafMotion:     vmafMotion,
			Subsampling:    vmafSubsampling,
		},
		ChromaWeight: chromaWeight,
	}
	scores, err := vmaf.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	PrettyPrintVmafScoreResult(scores)

}

func PrettyPrintVmafScoreResult(scores [4][]float64) {
	fmt.Printf("VMAF Score Result:\n\n")
	fmt.Printf("Y channel\n")
	PrettyPrintStats(CalculateStats(scores[0]))
	fmt.Printf("\nU channel\n")
	PrettyPrintStats(CalculateStats(scores[1]))
	fmt.Printf("\nV channel\n")
	PrettyPrintStats(CalculateStats(scores[2]))
	fmt.Printf("\nWeighted Scores\n")
	PrettyPrintStats(CalculateStats(scores[3]))
}

func CalculateStats(nums []float64) (mean, geoMean, min, max, stdDev float64) {
	if len(nums) == 0 {
		// Return NaN for all values if the slice is empty
		return math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()
	}

	sum := 0.0
	logSum := 0.0
	min = nums[0]
	max = nums[0]
	varianceSum := 0.0

	// Loop through the slice to calculate sum, logSum (for geometric mean), and min/max
	for _, num := range nums {
		// Update sum for mean
		sum += num

		logSum += math.Log(num)

		// Update min and max
		if num < min {
			min = num
		}
		if num > max {
			max = num
		}
	}

	// Calculate mean
	mean = sum / float64(len(nums))

	for _, num := range nums {
		varianceSum += (num - mean) * (num - mean)
	}

	// Calculate geometric mean (if valid)
	geoMean = math.Exp(logSum / float64(len(nums)))

	// Calculate standard deviation
	stdDev = varianceSum / float64(len(nums))

	return mean, geoMean, min, max, stdDev
}

func PrettyPrintStats(mean, geoMean, min, max, stdDev float64) {
	// Set a fixed width for all columns to ensure the numbers align
	const width = 20

	fmt.Printf("%-*s: %.2f\n", width, "Mean", mean)
	fmt.Printf("%-*s: %.2f\n", width, "Geometric Mean", geoMean)
	fmt.Printf("%-*s: %.2f\n", width, "Min", min)
	fmt.Printf("%-*s: %.2f\n", width, "Max", max)
	fmt.Printf("%-*s: %.2f\n", width, "Standard Deviation", stdDev)
}
