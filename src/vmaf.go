package src

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type VmafComputer struct {
	ReferencePath, DistortionPath string
	CompareChroma, VmafMotion     bool
	Subsampling                   int
}

func (vc *VmafComputer) Run() ([3][]float64, error) {
	var scores [3][]float64
	logFiles, err := vc.prepareLogFiles()
	if err != nil {
		return scores, err
	}
	defer cleanupLogFiles(logFiles)

	filter := vc.buildFilterGraph(logFiles)

	cmd := exec.Command(
		"ffmpeg", "-r", "1", "-i", vc.DistortionPath, "-r", "1", "-i", vc.ReferencePath,
		"-filter_complex", filter, "-f", "null", "-",
	)

	err = cmd.Run()
	if err != nil {
		return scores, err
	}

	return vc.parseScores(logFiles)
}

func (vf *VmafComputer) buildFilterGraph(logFiles []string) string {
	var filter strings.Builder
	filter.WriteString("[0:v:0]scale=1920:1080,format=yuv420p[dis];")
	filter.WriteString("[1:v:0]scale=1920:1080,format=yuv420p[ref];")

	if vf.CompareChroma {
		goto CHROMA
	}

	filter.WriteString("[dis][ref]libvmaf=" + vf.filterParams(logFiles[0]))

	goto RETURN

CHROMA:

	filter.WriteString("[dis]extractplanes=y+u+v[dis_0][dis_1][dis_2];")
	filter.WriteString("[ref]extractplanes=y+u+v[ref_0][ref_1][ref_2];")
	for i := 0; i < 3; i++ {
		filter.WriteString(fmt.Sprintf("[dis_%d]scale=1920:1080[dis_%d];",
			i, i))
		filter.WriteString(fmt.Sprintf("[ref_%d]scale=1920:1080[ref_%d];",
			i, i))
		filter.WriteString(fmt.Sprintf("[dis_%d][ref_%d]libvmaf=%s;",
			i, i, vf.filterParams(logFiles[i])))
	}

RETURN:

	return filter.String()
}

func (vc *VmafComputer) filterParams(logPath string) string {
	threads := runtime.NumCPU()
	if vc.CompareChroma {
		threads = threads/3 + 2
	}
	var param strings.Builder
	param.WriteString("n_threads=%d:log_fmt=json:log_path=%s:")
	param.WriteString("model=version=vmaf_v0.6.1\\\\:")
	param.WriteString("motion.motion_force_zero=%v:")
	param.WriteString("n_subsample=%d")

	return fmt.Sprintf(param.String(),
		threads, logPath, !vc.VmafMotion, vc.Subsampling,
	)
}

func (vc *VmafComputer) prepareLogFiles() ([]string, error) {
	numFiles := map[bool]int{true: 3, false: 1}[vc.CompareChroma]
	logFiles := make([]string, 0, numFiles)
	for i := 0; i < numFiles; i++ {
		file, err := os.CreateTemp("", "vmaf_log_*.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create temporary file: %w", err)
		}
		err = file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to close temporary file: %w", err)
		}
		logFiles = append(logFiles, toUnixPath(file.Name()))
	}
	return logFiles, nil
}

func (vc *VmafComputer) parseScores(logFiles []string) ([3][]float64, error) {
	var scores [3][]float64
	for i, logFile := range logFiles {
		parsed, err := parseVmafLogFile(logFile)
		if err != nil {
			return scores, fmt.Errorf("error parsing log file: %w", err)
		}
		scores[i] = parsed
	}
	return scores, nil
}

func parseVmafLogFile(logFile string) ([]float64, error) {
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

func cleanupLogFiles(logFiles []string) {
	for _, file := range logFiles {
		_ = os.Remove(file)
	}
}

func toUnixPath(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(strings.Split(path, ":")[1], "\\", "/")
	} else {
		return path
	}
}
