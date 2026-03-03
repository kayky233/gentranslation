package fasterwhisper

import (
	"encoding/json"
	"errors"
	"fmt"
	"krillin-ai/config"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

type runPlan struct {
	Model       string
	Device      string
	ComputeType string
	Reason      string
}

func buildRunPlans(model string) []runPlan {
	plans := make([]runPlan, 0, 6)
	if config.Conf.Transcribe.EnableGpuAcceleration {
		plans = append(plans,
			runPlan{Model: model, Device: "cuda", ComputeType: "float16", Reason: "gpu_primary"},
			runPlan{Model: model, Device: "cuda", ComputeType: "int8_float16", Reason: "gpu_quantized"},
			runPlan{Model: model, Device: "cpu", ComputeType: "int8", Reason: "cpu_fallback"},
		)
	} else {
		// Important: the standalone binary defaults to CUDA when available,
		// so we must explicitly pin CPU when GPU acceleration is disabled.
		plans = append(plans,
			runPlan{Model: model, Device: "cpu", ComputeType: "int8", Reason: "cpu_primary"},
		)
	}

	if model != "medium" && modelFileExists("medium") {
		plans = append(plans, runPlan{Model: "medium", Device: "cpu", ComputeType: "int8", Reason: "model_downgrade_medium"})
	}
	if model != "tiny" && modelFileExists("tiny") {
		plans = append(plans, runPlan{Model: "tiny", Device: "cpu", ComputeType: "int8", Reason: "model_downgrade_tiny"})
	}

	// deduplicate plan combinations
	unique := make([]runPlan, 0, len(plans))
	seen := make(map[string]struct{}, len(plans))
	for _, p := range plans {
		key := p.Model + "|" + p.Device + "|" + p.ComputeType
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, p)
	}
	return unique
}

func modelFileExists(model string) bool {
	path := filepath.Join("./models", "faster-whisper-"+model, "model.bin")
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func buildCmdArgs(audioFile, language, workDir string, plan runPlan) []string {
	return []string{
		"--model_dir", "./models/",
		"--model", plan.Model,
		"--one_word", "2",
		"--output_format", "json",
		"--language", language,
		"--output_dir", workDir,
		"--device", plan.Device,
		"--compute_type", plan.ComputeType,
		audioFile,
	}
}

func runSuccess(output []byte, err error) bool {
	if err == nil {
		return true
	}
	out := strings.ToLower(string(output))
	return strings.Contains(out, "subtitles are written to")
}

func isMemoryOrOOMError(output []byte) bool {
	out := strings.ToLower(string(output))
	patterns := []string{
		"mkl_malloc",
		"failed to allocate memory",
		"out of memory",
		"cuda out of memory",
		"std::bad_alloc",
	}
	for _, p := range patterns {
		if strings.Contains(out, p) {
			return true
		}
	}
	return false
}

func tailOutput(output []byte, maxLen int) string {
	detail := strings.TrimSpace(string(output))
	if len(detail) <= maxLen {
		return detail
	}
	return "... " + detail[len(detail)-maxLen:]
}

func (c *FastwhisperProcessor) Transcription(audioFile, language, workDir string) (*types.TranscriptionData, error) {
	plans := buildRunPlans(c.Model)
	if len(plans) == 0 {
		return nil, errors.New("faster-whisper run plans is empty")
	}

	var (
		lastErr    error
		lastOutput []byte
		lastPlan   runPlan
	)
	for idx, plan := range plans {
		cmdArgs := buildCmdArgs(audioFile, language, workDir, plan)
		cmd := exec.Command(storage.FasterwhisperPath, cmdArgs...)
		log.GetLogger().Info(
			"FastwhisperProcessor transcription start",
			zap.Int("attempt", idx+1),
			zap.Int("totalAttempts", len(plans)),
			zap.String("model", plan.Model),
			zap.String("device", plan.Device),
			zap.String("computeType", plan.ComputeType),
			zap.String("reason", plan.Reason),
			zap.String("cmd", cmd.String()),
		)

		output, err := cmd.CombinedOutput()
		if runSuccess(output, err) {
			if plan.Model != c.Model {
				log.GetLogger().Warn("FastwhisperProcessor model fallback succeeded", zap.String("from", c.Model), zap.String("to", plan.Model))
			}
			return parseTranscriptionResult(audioFile)
		}

		lastErr = err
		lastOutput = output
		lastPlan = plan
		log.GetLogger().Error(
			"FastwhisperProcessor cmd failed",
			zap.Int("attempt", idx+1),
			zap.String("model", plan.Model),
			zap.String("device", plan.Device),
			zap.String("computeType", plan.ComputeType),
			zap.String("reason", plan.Reason),
			zap.String("output", string(output)),
			zap.Error(err),
		)

		if !isMemoryOrOOMError(output) {
			break
		}
		log.GetLogger().Warn("FastwhisperProcessor memory pressure detected, trying next fallback plan", zap.String("reason", plan.Reason))
	}

	if lastErr == nil {
		lastErr = errors.New("unknown faster-whisper error")
	}
	return nil, fmt.Errorf(
		"faster-whisper transcription failed (model=%s, device=%s, compute_type=%s): %w; output: %s",
		lastPlan.Model,
		lastPlan.Device,
		lastPlan.ComputeType,
		lastErr,
		tailOutput(lastOutput, 1200),
	)
}

func parseTranscriptionResult(audioFile string) (*types.TranscriptionData, error) {
	resultFile := util.ChangeFileExtension(audioFile, ".json")
	fileData, err := os.Open(resultFile)
	if err != nil {
		log.GetLogger().Error("FastwhisperProcessor open json failed", zap.String("resultFile", resultFile), zap.Error(err))
		return nil, err
	}
	defer fileData.Close()

	var result types.FasterWhisperOutput
	decoder := json.NewDecoder(fileData)
	if err = decoder.Decode(&result); err != nil {
		log.GetLogger().Error("FastwhisperProcessor decode json failed", zap.String("resultFile", resultFile), zap.Error(err))
		return nil, err
	}

	transcriptionData := &types.TranscriptionData{}
	num := 0
	for _, segment := range result.Segments {
		transcriptionData.Text += segment.Text
		for _, word := range segment.Words {
			cleanWord := util.CleanPunction(strings.TrimSpace(word.Word))
			transcriptionData.Words = append(transcriptionData.Words, types.Word{
				Num:   num,
				Text:  cleanWord,
				Start: word.Start,
				End:   word.End,
			})
			num++
		}
	}

	log.GetLogger().Info("FastwhisperProcessor transcription success", zap.String("resultFile", resultFile))
	return transcriptionData, nil
}
