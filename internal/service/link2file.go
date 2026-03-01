package service

import (
	"context"
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

// isVideoExt 根据扩展名判断是否为视频文件
func isVideoExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".mp4" || ext == ".mkv" || ext == ".avi" || ext == ".mov" || ext == ".webm" || ext == ".flv"
}

// isAudioExt 根据扩展名判断是否为音频文件
func isAudioExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".mp3" || ext == ".wav" || ext == ".m4a" || ext == ".aac" || ext == ".flac" || ext == ".ogg"
}

func (s Service) linkToFile(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	var (
		err    error
		output []byte
	)
	link := stepParam.Link
	audioPath := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskAudioFileName)
	videoPath := fmt.Sprintf("%s/%s", stepParam.TaskBasePath, types.SubtitleTaskVideoFileName)
	stepParam.TaskPtr.ProcessPct = 3
	if strings.HasPrefix(link, "local_dual:") {
		// 视频和音频分开的两个文件，格式：local_dual:path1|path2（自动根据扩展名识别）
		paths := strings.SplitN(strings.TrimPrefix(link, "local_dual:"), "|", 2)
		if len(paths) != 2 {
			return errors.New("linkToFile error: local_dual 格式应为 local_dual:视频路径|音频路径")
		}
		var srcVideo, srcAudio string
		if isVideoExt(paths[0]) && isAudioExt(paths[1]) {
			srcVideo, srcAudio = paths[0], paths[1]
		} else if isVideoExt(paths[1]) && isAudioExt(paths[0]) {
			srcVideo, srcAudio = paths[1], paths[0]
		} else {
			return errors.New("linkToFile error: local_dual 需要一个是视频文件(.mp4/.mkv等)、一个是音频文件(.mp3/.wav等)")
		}
		// 用 ffmpeg 将音频转为目标格式
		cmd := exec.Command(storage.FfmpegPath, "-i", srcAudio, "-ar", "44100", "-ac", "2", "-ab", "192k", "-f", "mp3", "-y", audioPath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile local_dual ffmpeg audio error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("音频转换失败: %w", err)
		}
		stepParam.InputVideoPath = srcVideo
	} else if strings.HasPrefix(link, "local:") {
		// 本地单个文件（视频或音频）
		localPath := strings.TrimPrefix(link, "local:")
		cmd := exec.Command(storage.FfmpegPath, "-i", localPath, "-vn", "-ar", "44100", "-ac", "2", "-ab", "192k", "-f", "mp3", "-y", audioPath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("generateAudioSubtitles.linkToFile ffmpeg error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("generateAudioSubtitles.linkToFile ffmpeg error: %w", err)
		}
		if isVideoExt(localPath) {
			stepParam.InputVideoPath = localPath
		} else {
			stepParam.InputVideoPath = ""
		}
	} else if strings.Contains(link, "youtube.com") {
		var videoId string
		videoId, err = util.GetYouTubeID(link)
		if err != nil {
			log.GetLogger().Error("linkToFile.GetYouTubeID error", zap.Any("step param", stepParam), zap.Error(err))
			return fmt.Errorf("linkToFile.GetYouTubeID error: %w", err)
		}
		stepParam.Link = "https://www.youtube.com/watch?v=" + videoId
		// 使用更灵活的音频格式选择器，避免 HTTP 403 错误
		cmdArgs := []string{
			"-f", "bestaudio[ext=m4a]/bestaudio[ext=mp3]/bestaudio/worst",
			"--extract-audio",
			"--audio-format", "mp3",
			"--audio-quality", "192K",
			"-o", audioPath,
			stepParam.Link,
		}
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		if _, err := os.Stat("./cookies.txt"); err == nil {
			cmdArgs = append(cmdArgs, "--cookies", "./cookies.txt")
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("linkToFile download audio yt-dlp error: %w", err)
		}
	} else if strings.Contains(link, "bilibili.com") {
		videoId := util.GetBilibiliVideoId(link)
		if videoId == "" {
			return errors.New("linkToFile error: invalid bilibili link")
		}
		stepParam.Link = "https://www.bilibili.com/video/" + videoId
		// B 站格式较多样，使用灵活选择器避免无可用格式
		cmdArgs := []string{
			"-f", "bestaudio[ext=m4a]/bestaudio[ext=mp3]/bestaudio/worst",
			"--extract-audio",
			"--audio-format", "mp3",
			"--audio-quality", "192K",
			"-o", audioPath,
			"--no-warnings",
			stepParam.Link,
		}
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		if _, statErr := os.Stat("./cookies.txt"); statErr == nil {
			cmdArgs = append(cmdArgs, "--cookies", "./cookies.txt")
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			errDetail := strings.TrimSpace(string(output))
			if len(errDetail) > 300 {
				errDetail = "... " + errDetail[len(errDetail)-300:]
			}
			hint := "可尝试: 1) 更新 yt-dlp（.\\bin\\yt-dlp.exe -U） 2) 检查链接是否有效"
			if strings.Contains(string(output), "412") || strings.Contains(string(output), "Precondition Failed") {
				hint = "B站返回 412，通常需要登录 Cookie。请将 cookies.txt 放到项目根目录，详见 docs/zh/faq.md"
			}
			return fmt.Errorf("B站下载失败。%s 详情: %s", hint, errDetail)
		}
	} else {
		log.GetLogger().Info("linkToFile.unsupported link type", zap.Any("step param", stepParam))
		return errors.New("linkToFile error: unsupported link, only support youtube, bilibili and local file")
	}
	stepParam.TaskPtr.ProcessPct = 6
	stepParam.AudioFilePath = audioPath

	needDownloadVideo := !strings.HasPrefix(link, "local:") && !strings.HasPrefix(link, "local_dual:") && stepParam.EmbedSubtitleVideoType != "none"
	if needDownloadVideo {
		// 需要下载原视频
		cmdArgs := []string{"-f", "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=720][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=480][ext=mp4]+bestaudio[ext=m4a]", "-o", videoPath, stepParam.Link}
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile download video yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("linkToFile download video yt-dlp error: %w", err)
		}
		stepParam.InputVideoPath = videoPath
	}

	// 更新字幕任务信息
	stepParam.TaskPtr.ProcessPct = 10
	return nil
}
