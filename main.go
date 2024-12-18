package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/pflag"
)

type ComputeVmaf struct {
	ReferencePath, DistortionPath string
	CompareChroma, VmafMotion     bool
	VmafSubsampling, ChromaWeight int
}

type VmafScoreResult struct {
	FinalScore                      float64
	Mean, GeoMean, Min, Max, StdDev [3]float64
}

func (cf *ComputeVmaf) Run() (VmafScoreResult, error) {
	logFiles, err := cf.prepareLogFiles()
	if err != nil {
		return VmafScoreResult{}, err
	}
	defer cf.cleanupLogFiles(logFiles)

	filter := cf.buildFilterGraph(logFiles)
	if err := cf.executeFFmpeg(filter); err != nil {
		return VmafScoreResult{}, err
	}

	scores, err := cf.parseScores(logFiles)
	if err != nil {
		return VmafScoreResult{}, err
	}

	return cf.calculateFinalScores(scores), nil
}

func (cf *ComputeVmaf) executeFFmpeg(filter string) error {
	cmd := exec.Command(
		"ffmpeg", "-r", "1", "-i", cf.DistortionPath, "-r", "1", "-i", cf.ReferencePath,
		"-filter_complex", filter, "-f", "null", "-",
	)
	return cmd.Run()
}

func (cf *ComputeVmaf) prepareLogFiles() ([]string, error) {
	numFiles := map[bool]int{true: 3, false: 1}[cf.CompareChroma]
	logFiles := make([]string, 0, numFiles)
	for i := 0; i < numFiles; i++ {
		file, err := os.CreateTemp("", "vmaf_log_*.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create temporary file: %w", err)
		}
		defer file.Close()
		logFiles = append(logFiles, cf.toUnixPath(file.Name()))
	}
	return logFiles, nil
}

func (cf *ComputeVmaf) buildFilterGraph(logFiles []string) string {
	var filter strings.Builder
	filter.WriteString("[0:v:0]scale=1920:1080,format=yuv420p[dis];[1:v:0]scale=1920:1080,format=yuv420p[ref];")

	if cf.CompareChroma {
		filter.WriteString("[dis]extractplanes=y+u+v[dis_0][dis_1][dis_2];[ref]extractplanes=y+u+v[ref_0][ref_1][ref_2];")
		for i := 0; i < 3; i++ {
			filter.WriteString(fmt.Sprintf("[dis_%d][ref_%d]libvmaf=%s;", i, i, cf.filterParams(logFiles[i])))
		}
	} else {
		filter.WriteString("[dis][ref]libvmaf=" + cf.filterParams(logFiles[0]))
	}
	return filter.String()
}

func (cf *ComputeVmaf) filterParams(logPath string) string {
	threads := runtime.NumCPU()
	if cf.CompareChroma {
		threads = threads/3 + 2
	}
	return fmt.Sprintf(
		"n_threads=%d:model=version=vmaf_v0.6.1\\\\:motion.motion_force_zero=%v:log_fmt=json:log_path=%s",
		threads, !cf.VmafMotion, logPath,
	)
}

func (cf *ComputeVmaf) parseScores(logFiles []string) ([][]float64, error) {
	scores := make([][]float64, len(logFiles))
	for i, logFile := range logFiles {
		parsed, err := cf.parseLogFile(logFile)
		if err != nil {
			return nil, fmt.Errorf("error parsing log file: %w", err)
		}
		scores[i] = parsed
	}
	return scores, nil
}

func (*ComputeVmaf) parseLogFile(logFile string) ([]float64, error) {
	var log struct {
		Frames []struct {
			Metrics struct {
				Vmaf float64 `json:"vmaf"`
			} `json:"metrics"`
		} `json:"frames"`
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("error unmarshalling JSON: %w", err)
	}
	scores := make([]float64, len(log.Frames))
	for i, frame := range log.Frames {
		scores[i] = frame.Metrics.Vmaf
	}
	return scores, nil
}

func (cf *ComputeVmaf) calculateFinalScores(scores [][]float64) VmafScoreResult {
	var geoScores []float64
	var res VmafScoreResult
	for i, score := range scores {
		res.Mean[i], res.GeoMean[i], res.Min[i], res.Max[i], res.StdDev[i] = cf.stats(score)
		geoScores = append(geoScores, res.GeoMean[i])
	}
	if cf.CompareChroma {
		weight := float64(cf.ChromaWeight)
		res.FinalScore = (geoScores[0]*weight + geoScores[1] + geoScores[2]) / (weight + 2)
	} else {
		res.FinalScore = geoScores[0]
	}
	return res
}

func (*ComputeVmaf) stats(nums []float64) (float64, float64, float64, float64, float64) {
	if len(nums) == 0 {
		return 0, 0, 0, 0, 0
	}
	sum, logSum, min, max := 0.0, 0.0, nums[0], nums[0]
	for _, num := range nums {
		sum += num
		logSum += math.Log(num)
		if num < min {
			min = num
		}
		if num > max {
			max = num
		}
	}
	mean := sum / float64(len(nums))
	geomMean := math.Exp(logSum / float64(len(nums)))
	varianceSum := 0.0
	for _, num := range nums {
		diff := num - mean
		varianceSum += diff * diff
	}
	stdDev := math.Sqrt(varianceSum / float64(len(nums)))
	return mean, geomMean, min, max, stdDev
}

func (*ComputeVmaf) cleanupLogFiles(logFiles []string) {
	for _, file := range logFiles {
		_ = os.Remove(file)
	}
}

func (*ComputeVmaf) toUnixPath(path string) string {
	return strings.ReplaceAll(strings.Split(path, ":")[1], "\\", "/")
}

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
	pflag.IntVar(&chromaWeight, "chroma-weight", 4, "relative weight of chroma to luma channels (1/chroma-weight)")
	pflag.BoolVar(&vmafMotion, "vmaf-motion", false, "enable temporal VMAF scoring (not recommended for high-quality targets)")
	pflag.CommandLine.SortFlags = false
	pflag.Parse()

	compareChroma = !compareChromaNeg
}

func main() {
	vmaf := ComputeVmaf{
		ReferencePath:   reference,
		DistortionPath:  distortion,
		CompareChroma:   compareChroma,
		VmafSubsampling: vmafSubsampling,
		ChromaWeight:    chromaWeight,
	}
	scores, err := vmaf.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	PrettyPrintVmafScoreResult(scores)

}

func PrettyPrintVmafScoreResult(result VmafScoreResult) {
	fmt.Printf("VMAF Score Result:\n")
	fmt.Printf("  Final Score: %5.2f\n", result.FinalScore)
	fmt.Printf("                 Y      U      V\n")
	fmt.Printf("  Mean:      [%5.2f, %5.2f, %5.2f]\n", result.Mean[0], result.Mean[1], result.Mean[2])
	fmt.Printf("  GeoMean:   [%5.2f, %5.2f, %5.2f]\n", result.GeoMean[0], result.GeoMean[1], result.GeoMean[2])
	fmt.Printf("  Min:       [%5.2f, %5.2f, %5.2f]\n", result.Min[0], result.Min[1], result.Min[2])
	fmt.Printf("  Max:       [%5.2f, %5.2f, %5.2f]\n", result.Max[0], result.Max[1], result.Max[2])
	fmt.Printf("  StdDev:    [%5.2f, %5.2f, %5.2f]\n", result.StdDev[0], result.StdDev[1], result.StdDev[2])
}
