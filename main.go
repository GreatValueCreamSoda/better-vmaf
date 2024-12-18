package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

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
	pflag.StringVarP(&reference, "reference", "r", "", "reference video file for vmaf comaprison")
	pflag.StringVarP(&distortion, "distortion", "d", "", "distorted video file for vmaf comaprison")
	pflag.IntVar(&vmafSubsampling, "vmaf-subsampling", 1, "only calculate every X frame for faster comparisions")
	pflag.BoolVar(&compareChromaNeg, "no-compare-chroma", false, "compute vmaf scores without chroma channels (standalone vmaf defaults to this)")
	pflag.IntVar(&chromaWeight, "chroma-weight", 4, "sets the chroma weight relative to luma channels where weight is 1/chroma-weight")
	pflag.BoolVar(&vmafMotion, "vmaf-motion", false, "set whether or not to calculate vmaf using vmafs temporal component. Not recomended for high quality targets")
	pflag.CommandLine.SortFlags = false
	pflag.Parse()

	if compareChromaNeg {
		compareChroma = false
	} else {
		compareChroma = true
	}
}

func main() {
	var logFiles []string

	if compareChroma {
		lumaLog, err := os.CreateTemp("", "")
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		defer lumaLog.Close()
		defer os.Remove(lumaLog.Name())
		logFiles = append(logFiles, ConvertToUnixPath(lumaLog.Name()))
		ULog, err := os.CreateTemp("", "")
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		defer ULog.Close()
		defer os.Remove(ULog.Name())
		logFiles = append(logFiles, ConvertToUnixPath(ULog.Name()))
		VLog, err := os.CreateTemp("", "")
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		defer VLog.Close()
		defer os.Remove(VLog.Name())
		logFiles = append(logFiles, ConvertToUnixPath(VLog.Name()))
	} else {
		lumaLog, err := os.CreateTemp("", "")
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		defer lumaLog.Close()
		logFiles = append(logFiles, ConvertToUnixPath(lumaLog.Name()))
	}

	var vmafModelParam strings.Builder

	vmafModelParam.WriteString("n_threads=")

	if compareChroma {
		vmafModelParam.WriteString(strconv.Itoa(runtime.NumCPU()/3 + 2))
	} else {
		vmafModelParam.WriteString(strconv.Itoa(runtime.NumCPU()))
	}

	vmafModelParam.WriteString(":model=version=vmaf_v0.6.1")

	if !vmafMotion {
		vmafModelParam.WriteString("\\\\:motion.motion_force_zero=true")
	}

	vmafModelParam.WriteString(":log_fmt=json")

	vmafModelParamString := vmafModelParam.String()

	var filter strings.Builder

	filter.WriteString("[0:v:0]scale=1920:1080,format=yuv420p[dis];")
	filter.WriteString("[1:v:0]scale=1920:1080,format=yuv420p[ref];")

	if compareChroma {
		filter.WriteString("[dis]extractplanes=y+u+v[dis_y][dis_u][dis_v];")
		filter.WriteString("[ref]extractplanes=y+u+v[ref_y][ref_u][ref_v];")
		filter.WriteString("[dis_u]scale=1920:1080[dis_u];")
		filter.WriteString("[dis_v]scale=1920:1080[dis_v];")
		filter.WriteString("[ref_u]scale=1920:1080[ref_u];")
		filter.WriteString("[ref_v]scale=1920:1080[ref_v];")
		filter.WriteString("[dis_y][ref_y]libvmaf=")
		filter.WriteString(vmafModelParamString)
		filter.WriteString(":log_path=")
		filter.WriteString(logFiles[0])
		filter.WriteString(";")
		filter.WriteString("[dis_u][ref_u]libvmaf=")
		filter.WriteString(vmafModelParamString)
		filter.WriteString(":log_path=")
		filter.WriteString(logFiles[1])
		filter.WriteString(";")
		filter.WriteString("[dis_v][ref_v]libvmaf=")
		filter.WriteString(vmafModelParamString)
		filter.WriteString(":log_path=")
		filter.WriteString(logFiles[2])
		filter.WriteString(";")
	} else {
		filter.WriteString("[dis][ref]libvmaf=")
		filter.WriteString(vmafModelParamString)
		filter.WriteString(":log_path=")
		filter.WriteString(logFiles[0])
	}

	ffmpegCmd := exec.Command("ffmpeg",
		"-r", "1", "-i", distortion,
		"-r", "1", "-i", reference,
		"-filter_complex", filter.String(),
		"-f", "null", "null",
	)

	//fmt.Println(ffmpegCmd.Args)

	//ffmpegCmd.Stderr = os.Stderr

	err := ffmpegCmd.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	var scores [][]float64 = make([][]float64, len(logFiles))

	for i, file := range logFiles {
		var err error
		scores[i], err = parseVmafLogFile(file)
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
	}

	var geoScores []float64

	if compareChroma {
		for i, score := range scores {
			switch i {
			case 0:
				fmt.Println("y Vmaf:")
			case 1:
				fmt.Println("U Vmaf")
			case 2:
				fmt.Println("v Vmaf:")
			}
			_, geomean, _, _ := CalculateStats(score)
			geoScores = append(geoScores, geomean)
			fmt.Println("")
		}
	} else {
		fmt.Println("Vmaf:")
		_, geomean, _, _ := CalculateStats(scores[0])
		geoScores = append(geoScores, geomean)
		fmt.Println("")
	}
	var finalScore float64

	if len(geoScores) > 1 {
		finalScore = (geoScores[0]*float64(chromaWeight) + geoScores[1] + geoScores[2]) / (float64(chromaWeight) + 2)
	} else {
		finalScore = geoScores[0]
	}

	fmt.Printf("Final Score: %f", finalScore)
}

func parseVmafLogFile(logFile string) ([]float64, error) {
	var log struct {
		Frames []struct {
			Metrics struct {
				Vmaf float64 `json:"vmaf"`
			} `json:"metrics"`
		} `json:"frames"`
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		return nil, fmt.Errorf("error reading VMAF log file: %w", err)
	}

	if err := json.Unmarshal(logData, &log); err != nil {
		return nil, fmt.Errorf("error parsing VMAF log file: %w", err)
	}

	scores := make([]float64, len(log.Frames))
	for i, frame := range log.Frames {
		scores[i] = frame.Metrics.Vmaf
	}

	return scores, nil
}

func ConvertToUnixPath(winPath string) string {
	return strings.Split(strings.ReplaceAll(winPath, "\\", "/"), ":")[1]
}

func CalculateStats(numbers []float64) (float64, float64, float64, float64) {
	if len(numbers) == 0 {
		fmt.Println("Slice is empty. Cannot calculate statistics.")
		return 0, 0, 0, 0
	}

	var sum, logSum float64 = 0, 0
	min, max := numbers[0], numbers[0]

	for _, num := range numbers {
		sum += num
		logSum += math.Log(num)
		if num < min {
			min = num
		}
		if num > max {
			max = num
		}
	}

	mean := sum / float64(len(numbers))
	geomMean := math.Exp(logSum / float64(len(numbers)))

	var varianceSum float64
	for _, num := range numbers {
		diff := num - mean
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(numbers))

	stdDev := math.Sqrt(variance)
	fmt.Printf("Mean: %.2f, Geometric Mean: %.2f, Min: %.2f, Max: %.2f, StdDev: %.2f\n", mean, geomMean, min, max, stdDev)

	return mean, geomMean, min, max
}
