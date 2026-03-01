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

func isYouTubeLink(link string) bool {
	l := strings.ToLower(strings.TrimSpace(link))
	return strings.Contains(l, "youtube.com") || strings.Contains(l, "youtu.be")
}

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

func bilibiliAnti412Args() []string {
	return []string{
		"--add-header", "Referer: https://www.bilibili.com/",
		"--add-header", "Origin: https://www.bilibili.com",
		"--add-header", "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
	}
}

func isBilibili412(output []byte) bool {
	out := string(output)
	return strings.Contains(out, "412") || strings.Contains(out, "Precondition Failed")
}

func tailOutput(output []byte, maxLen int) string {
	detail := strings.TrimSpace(string(output))
	if len(detail) <= maxLen {
		return detail
	}
	return "... " + detail[len(detail)-maxLen:]
}

func shouldRetryYouTubeAfterUpdate(output []byte) bool {
	out := strings.ToLower(string(output))
	return strings.Contains(out, "nsig extraction failed") ||
		strings.Contains(out, "precondition check failed") ||
		strings.Contains(out, "unable to extract") ||
		strings.Contains(out, "http error 400") ||
		strings.Contains(out, "sign in to confirm")
}

func youtubeDownloadHint(output []byte, cookiesPath string) string {
	out := strings.ToLower(string(output))
	if strings.Contains(out, "sign in to confirm") || strings.Contains(out, "not a bot") {
		if _, statErr := os.Stat(cookiesPath); statErr == nil {
			return "YouTube 触发登录校验，当前 cookies 可能已失效。请重新导出 YouTube cookies.txt 后重试。"
		}
		return "YouTube 触发登录校验，请上传有效的 YouTube cookies.txt 后重试。"
	}
	if strings.Contains(out, "nsig extraction failed") || strings.Contains(out, "precondition check failed") {
		return "yt-dlp 版本可能过旧或接口策略变化，建议升级后重试。"
	}
	return "可尝试：1) 重试任务 2) 上传 YouTube cookies.txt 3) 检查代理网络。"
}

func resolveCookiesPath(stepParam *types.SubtitleTaskStepParam) string {
	if stepParam != nil && strings.TrimSpace(stepParam.CookiesFilePath) != "" {
		return strings.TrimSpace(stepParam.CookiesFilePath)
	}
	return "./cookies.txt"
}

func hasBilibiliSessdataCookie(cookiesPath string) bool {
	data, err := os.ReadFile(cookiesPath)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "bilibili.com") && strings.Contains(content, "SESSDATA")
}

func bilibiliDownloadHint(output []byte, cookiesPath string) string {
	out := string(output)
	if strings.Contains(out, "Failed to decrypt with DPAPI") ||
		strings.Contains(out, "Could not copy Chrome cookie database") ||
		strings.Contains(out, "no such table: moz_cookies") {
		return "自动读取浏览器 Cookie 失败（浏览器可能占用或系统加密限制）。请关闭浏览器后重试，或手动导出 cookies.txt（可在网页上传，仅当前任务使用）"
	}
	if isBilibili412(output) {
		if _, statErr := os.Stat(cookiesPath); statErr == nil && !hasBilibiliSessdataCookie(cookiesPath) {
			return "检测到 cookies.txt 但缺少 B 站登录态（SESSDATA）。请先在浏览器登录 B站，再重新导出 cookies.txt（可在网页上传，仅当前任务使用）"
		}
		return "B站返回 412，通常需要登录 Cookie。已自动尝试读取浏览器 Cookie；若仍失败，请重新导出 cookies.txt（可在网页上传，仅当前任务使用）"
	}
	return "可尝试: 1) 更新 yt-dlp（.\\bin\\yt-dlp.exe -U） 2) 检查链接是否有效"
}

func runYtDlpWithBilibiliFallback(baseArgs []string, url string, cookiesPath string) ([]byte, error) {
	args := append([]string{}, baseArgs...)
	if _, statErr := os.Stat(cookiesPath); statErr == nil {
		args = append(args, "--cookies", cookiesPath)
	}
	args = append(args, url)

	cmd := exec.Command(storage.YtdlpPath, args...)
	output, err := cmd.CombinedOutput()
	if err == nil || !isBilibili412(output) {
		return output, err
	}

	// 412 时尝试直接读取本机浏览器 Cookie（依次 edge/chrome/firefox）
	combinedOutput := string(output)
	for _, browser := range []string{"edge", "chrome", "firefox"} {
		retryArgs := append([]string{}, baseArgs...)
		retryArgs = append(retryArgs, "--cookies-from-browser", browser, url)
		retryCmd := exec.Command(storage.YtdlpPath, retryArgs...)
		retryOutput, retryErr := retryCmd.CombinedOutput()
		if retryErr == nil {
			return retryOutput, nil
		}
		if len(retryOutput) > 0 {
			combinedOutput += "\n[retry " + browser + "] " + string(retryOutput)
		}
		err = retryErr
		output = retryOutput
	}

	if combinedOutput != "" {
		return []byte(combinedOutput), err
	}
	return output, err
}

func (s Service) linkToFile(ctx context.Context, stepParam *types.SubtitleTaskStepParam) error {
	var (
		err    error
		output []byte
	)
	cookiesPath := resolveCookiesPath(stepParam)
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
	} else if isYouTubeLink(link) {
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
		if _, err := os.Stat(cookiesPath); err == nil {
			cmdArgs = append(cmdArgs, "--cookies", cookiesPath)
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil && shouldRetryYouTubeAfterUpdate(output) {
			updateCmd := exec.Command(storage.YtdlpPath, "-U")
			updateOutput, updateErr := updateCmd.CombinedOutput()
			if updateErr != nil {
				log.GetLogger().Warn("linkToFile yt-dlp self-update failed", zap.String("updateOutput", string(updateOutput)), zap.Error(updateErr))
			} else {
				log.GetLogger().Info("linkToFile yt-dlp self-update succeeded", zap.String("updateOutput", string(updateOutput)))
			}
			cmd = exec.Command(storage.YtdlpPath, cmdArgs...)
			retryOutput, retryErr := cmd.CombinedOutput()
			if retryErr == nil {
				output = retryOutput
				err = nil
			} else {
				output = append(output, []byte("\n[retry after -U]\n"+string(retryOutput))...)
				err = retryErr
			}
		}
		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			hint := youtubeDownloadHint(output, cookiesPath)
			return fmt.Errorf("YouTube 下载音频失败。%s 详情: %s", hint, tailOutput(output, 600))
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
		}
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		cmdArgs = append(cmdArgs, bilibiliAnti412Args()...)
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		output, err = runYtDlpWithBilibiliFallback(cmdArgs, stepParam.Link, cookiesPath)
		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			errDetail := strings.TrimSpace(string(output))
			if len(errDetail) > 300 {
				errDetail = "... " + errDetail[len(errDetail)-300:]
			}
			hint := bilibiliDownloadHint(output, cookiesPath)
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
		cmdArgs := []string{"-f", "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=720][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=480][ext=mp4]+bestaudio[ext=m4a]", "-o", videoPath}
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		if strings.Contains(stepParam.Link, "bilibili.com") {
			cmdArgs = append(cmdArgs, bilibiliAnti412Args()...)
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		if strings.Contains(stepParam.Link, "bilibili.com") {
			output, err = runYtDlpWithBilibiliFallback(cmdArgs, stepParam.Link, cookiesPath)
		} else {
			args := append([]string{}, cmdArgs...)
			args = append(args, stepParam.Link)
			cmd := exec.Command(storage.YtdlpPath, args...)
			output, err = cmd.CombinedOutput()
		}
		if err != nil {
			log.GetLogger().Error("linkToFile download video yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			if strings.Contains(stepParam.Link, "bilibili.com") {
				errDetail := strings.TrimSpace(string(output))
				if len(errDetail) > 300 {
					errDetail = "... " + errDetail[len(errDetail)-300:]
				}
				hint := bilibiliDownloadHint(output, cookiesPath)
				return fmt.Errorf("B站视频下载失败。%s 详情: %s", hint, errDetail)
			}
			return fmt.Errorf("linkToFile download video yt-dlp error: %w", err)
		}
		stepParam.InputVideoPath = videoPath
	}

	// 更新字幕任务信息
	stepParam.TaskPtr.ProcessPct = 10
	return nil
}
