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

func isBilibiliLink(link string) bool {
	l := strings.ToLower(strings.TrimSpace(link))
	return strings.Contains(l, "bilibili.com")
}

func isTwitterLink(link string) bool {
	l := strings.ToLower(strings.TrimSpace(link))
	return strings.Contains(l, "x.com") || strings.Contains(l, "twitter.com")
}

func isVideoExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".mp4" || ext == ".mkv" || ext == ".avi" || ext == ".mov" || ext == ".webm" || ext == ".flv"
}

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

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absPath)
}

func resolveFirstExistingPath(paths ...string) string {
	for _, p := range paths {
		normalizedPath := normalizePath(p)
		if fileExists(normalizedPath) {
			return normalizedPath
		}
	}
	return ""
}

func isYouTubeSignInChallenge(output []byte) bool {
	out := strings.ToLower(string(output))
	return strings.Contains(out, "sign in to confirm") || strings.Contains(out, "not a bot")
}

func defaultCookiesCandidates() []string {
	candidates := make([]string, 0, 8)
	for _, envKey := range []string{"KRILLIN_YOUTUBE_COOKIES_PATH", "KRILLIN_COOKIES_FILE", "YT_DLP_COOKIES"} {
		if envPath := strings.TrimSpace(os.Getenv(envKey)); envPath != "" {
			candidates = append(candidates, envPath)
		}
	}
	candidates = append(candidates, "./cookies.txt", "./config/cookies.txt")
	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executablePath), "cookies.txt"))
	}
	return candidates
}

func shouldRetryYouTubeAfterUpdate(output []byte) bool {
	out := strings.ToLower(string(output))
	return strings.Contains(out, "nsig extraction failed") ||
		strings.Contains(out, "precondition check failed") ||
		strings.Contains(out, "unable to extract") ||
		strings.Contains(out, "http error 400")
}

func youtubeDownloadHint(output []byte, cookiesPath string) string {
	out := strings.ToLower(string(output))
	if isYouTubeSignInChallenge(output) {
		if fileExists(cookiesPath) {
			return "YouTube 触发登录校验，当前 cookies 可能已失效。请重新导出 YouTube cookies.txt 后重试。"
		}
		return fmt.Sprintf("YouTube 触发登录校验，未找到可用 cookies 文件（当前路径：%s）。请上传有效的 YouTube cookies.txt，或设置环境变量 KRILLIN_YOUTUBE_COOKIES_PATH 后重试。", cookiesPath)
	}
	if strings.Contains(out, "nsig extraction failed") || strings.Contains(out, "precondition check failed") {
		return "yt-dlp 版本可能过旧或接口策略变化，建议升级后重试。"
	}
	return "可尝试：1) 重试任务 2) 上传 YouTube cookies.txt 3) 检查代理网络。"
}

func buildYouTubeDownloadArgs(link, audioPath, cookiesPath string) []string {
	args := []string{
		"-f", "bestaudio[ext=m4a]/bestaudio[ext=mp3]/bestaudio/worst",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "192K",
		"-o", audioPath,
	}
	if config.Conf.App.Proxy != "" {
		args = append(args, "--proxy", config.Conf.App.Proxy)
	}
	if fileExists(cookiesPath) {
		args = append(args, "--cookies", cookiesPath)
	}
	if storage.FfmpegPath != "ffmpeg" {
		args = append(args, "--ffmpeg-location", storage.FfmpegPath)
	}
	args = append(args, link)
	return args
}

func buildYouTubeFallbackArgs(link, audioPath, cookiesPath string) []string {
	args := []string{
		"--extractor-args", "youtube:player_client=tv,ios,android",
		"--add-header", "Accept-Language:en-US,en;q=0.9",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
		"-f", "bestaudio[ext=m4a]/bestaudio[ext=mp3]/bestaudio/18/worst",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "192K",
		"-o", audioPath,
	}
	if config.Conf.App.Proxy != "" {
		args = append(args, "--proxy", config.Conf.App.Proxy)
	}
	if fileExists(cookiesPath) {
		args = append(args, "--cookies", cookiesPath)
	}
	if storage.FfmpegPath != "ffmpeg" {
		args = append(args, "--ffmpeg-location", storage.FfmpegPath)
	}
	args = append(args, link)
	return args
}

func resolveCookiesPath(stepParam *types.SubtitleTaskStepParam) string {
	if stepParam != nil {
		if taskCookiesPath := strings.TrimSpace(stepParam.CookiesFilePath); taskCookiesPath != "" {
			if resolvedPath := resolveFirstExistingPath(taskCookiesPath); resolvedPath != "" {
				return resolvedPath
			}
			return normalizePath(taskCookiesPath)
		}
	}
	if resolvedPath := resolveFirstExistingPath(defaultCookiesCandidates()...); resolvedPath != "" {
		return resolvedPath
	}
	return normalizePath("./cookies.txt")
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
		if fileExists(cookiesPath) && !hasBilibiliSessdataCookie(cookiesPath) {
			return "检测到 cookies.txt 但缺少 B站登录态（SESSDATA）。请先在浏览器登录 B站，再重新导出 cookies.txt（可在网页上传，仅当前任务使用）"
		}
		return "B站返回 412，通常需要登录 Cookie。已自动尝试读取浏览器 Cookie；若仍失败，请重新导出 cookies.txt（可在网页上传，仅当前任务使用）"
	}
	return "可尝试: 1) 更新 yt-dlp（.\\bin\\yt-dlp.exe -U） 2) 检查链接是否有效"
}

func isTwitterAuthRequired(output []byte) bool {
	out := strings.ToLower(string(output))
	return strings.Contains(out, "login required") ||
		strings.Contains(out, "authorization required") ||
		strings.Contains(out, "sign in") ||
		strings.Contains(out, "sensitive media")
}

func twitterDownloadHint(output []byte, cookiesPath string) string {
	out := strings.ToLower(string(output))
	if isTwitterAuthRequired(output) {
		if fileExists(cookiesPath) {
			return "Twitter/X requires login for this video, and the current cookies may be invalid."
		}
		return fmt.Sprintf("Twitter/X may require login for this video. Upload a valid cookies.txt and retry (current path: %s).", cookiesPath)
	}
	if strings.Contains(out, "no video could be found") || strings.Contains(out, "unsupported url") {
		return "Please make sure the link is a single tweet/status page that contains a video."
	}
	return "Try: 1) verify this tweet has video 2) upload Twitter/X cookies.txt 3) update yt-dlp (./bin/yt-dlp.exe -U)."
}

func isRequestedFormatUnavailable(output []byte) bool {
	out := strings.ToLower(string(output))
	return strings.Contains(out, "requested format is not available")
}

func runYtDlpWithBilibiliFallback(baseArgs []string, url string, cookiesPath string) ([]byte, error) {
	args := append([]string{}, baseArgs...)
	if fileExists(cookiesPath) {
		args = append(args, "--cookies", cookiesPath)
	}
	args = append(args, url)

	cmd := exec.Command(storage.YtdlpPath, args...)
	output, err := cmd.CombinedOutput()
	if err == nil || !isBilibili412(output) {
		return output, err
	}

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

func runYtDlpWithCookiesFromBrowser(baseArgs []string, url string) ([]byte, error) {
	argsWithoutURL := append([]string{}, baseArgs...)
	if len(argsWithoutURL) > 0 && argsWithoutURL[len(argsWithoutURL)-1] == url {
		argsWithoutURL = argsWithoutURL[:len(argsWithoutURL)-1]
	}

	var (
		combinedOutput strings.Builder
		lastOutput     []byte
		lastErr        error
	)
	for _, browser := range []string{"edge", "chrome", "firefox"} {
		retryArgs := append([]string{}, argsWithoutURL...)
		retryArgs = append(retryArgs, "--cookies-from-browser", browser, url)
		retryCmd := exec.Command(storage.YtdlpPath, retryArgs...)
		retryOutput, retryErr := retryCmd.CombinedOutput()
		if retryErr == nil {
			return retryOutput, nil
		}
		if len(retryOutput) > 0 {
			combinedOutput.WriteString("\n[cookies-from-browser ")
			combinedOutput.WriteString(browser)
			combinedOutput.WriteString("] ")
			combinedOutput.WriteString(string(retryOutput))
		}
		lastErr = retryErr
		lastOutput = retryOutput
	}
	if combinedOutput.Len() > 0 {
		return []byte(combinedOutput.String()), lastErr
	}
	return lastOutput, lastErr
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
		paths := strings.SplitN(strings.TrimPrefix(link, "local_dual:"), "|", 2)
		if len(paths) != 2 {
			return errors.New("linkToFile error: local_dual format should be local_dual:video_path|audio_path")
		}
		var srcVideo, srcAudio string
		if isVideoExt(paths[0]) && isAudioExt(paths[1]) {
			srcVideo, srcAudio = paths[0], paths[1]
		} else if isVideoExt(paths[1]) && isAudioExt(paths[0]) {
			srcVideo, srcAudio = paths[1], paths[0]
		} else {
			return errors.New("linkToFile error: local_dual requires one video file and one audio file")
		}

		cmd := exec.Command(storage.FfmpegPath, "-i", srcAudio, "-ar", "44100", "-ac", "2", "-ab", "192k", "-f", "mp3", "-y", audioPath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			log.GetLogger().Error("linkToFile local_dual ffmpeg audio error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			return fmt.Errorf("音频转换失败: %w", err)
		}
		stepParam.InputVideoPath = srcVideo
	} else if strings.HasPrefix(link, "local:") {
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
		videoID, err := util.GetYouTubeID(link)
		if err != nil {
			log.GetLogger().Error("linkToFile.GetYouTubeID error", zap.Any("step param", stepParam), zap.Error(err))
			return fmt.Errorf("linkToFile.GetYouTubeID error: %w", err)
		}
		stepParam.Link = "https://www.youtube.com/watch?v=" + videoID

		cmdArgs := buildYouTubeDownloadArgs(stepParam.Link, audioPath, cookiesPath)
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

		if err != nil && isYouTubeSignInChallenge(output) {
			fallbackArgs := buildYouTubeFallbackArgs(stepParam.Link, audioPath, cookiesPath)
			fallbackCmd := exec.Command(storage.YtdlpPath, fallbackArgs...)
			fallbackOutput, fallbackErr := fallbackCmd.CombinedOutput()
			if fallbackErr == nil {
				output = fallbackOutput
				err = nil
			} else {
				output = append(output, []byte("\n[fallback client args]\n"+string(fallbackOutput))...)
				err = fallbackErr
			}
		}

		if err != nil && isYouTubeSignInChallenge(output) {
			browserFallbackArgs := buildYouTubeFallbackArgs(stepParam.Link, audioPath, "")
			browserFallbackOutput, browserFallbackErr := runYtDlpWithCookiesFromBrowser(browserFallbackArgs, stepParam.Link)
			if browserFallbackErr == nil {
				output = browserFallbackOutput
				err = nil
			} else {
				output = append(output, []byte("\n[fallback cookies-from-browser]\n"+string(browserFallbackOutput))...)
				err = browserFallbackErr
			}
		}

		if err != nil {
			log.GetLogger().Error("linkToFile download audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			hint := youtubeDownloadHint(output, cookiesPath)
			return fmt.Errorf("YouTube 下载音频失败。%s 详情: %s", hint, tailOutput(output, 600))
		}
	} else if isBilibiliLink(link) {
		videoID := util.GetBilibiliVideoId(link)
		if videoID == "" {
			return errors.New("linkToFile error: invalid bilibili link")
		}
		stepParam.Link = "https://www.bilibili.com/video/" + videoID
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
	} else if isTwitterLink(link) {
		statusID := util.GetTwitterStatusID(link)
		if statusID == "" {
			return errors.New("linkToFile error: invalid twitter link")
		}
		stepParam.Link = "https://x.com/i/status/" + statusID
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
		if fileExists(cookiesPath) {
			cmdArgs = append(cmdArgs, "--cookies", cookiesPath)
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}
		cmdArgs = append(cmdArgs, stepParam.Link)

		cmd := exec.Command(storage.YtdlpPath, cmdArgs...)
		output, err = cmd.CombinedOutput()
		if err != nil && isTwitterAuthRequired(output) {
			retryOutput, retryErr := runYtDlpWithCookiesFromBrowser(cmdArgs, stepParam.Link)
			if retryErr == nil {
				output = retryOutput
				err = nil
			} else {
				output = append(output, []byte("\n[fallback cookies-from-browser]\n"+string(retryOutput))...)
				err = retryErr
			}
		}
		if err != nil {
			log.GetLogger().Error("linkToFile download twitter audio yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			hint := twitterDownloadHint(output, cookiesPath)
			return fmt.Errorf("Twitter/X download audio failed. %s Details: %s", hint, tailOutput(output, 600))
		}
	} else {
		log.GetLogger().Info("linkToFile.unsupported link type", zap.Any("step param", stepParam))
		return errors.New("linkToFile error: unsupported link, only support youtube, bilibili, twitter/x and local file")
	}

	stepParam.TaskPtr.ProcessPct = 6
	stepParam.AudioFilePath = audioPath

	needDownloadVideo := !strings.HasPrefix(link, "local:") && !strings.HasPrefix(link, "local_dual:") && stepParam.EmbedSubtitleVideoType != "none"
	if needDownloadVideo {
		cmdArgs := []string{"-f", "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=720][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=480][ext=mp4]+bestaudio[ext=m4a]", "-o", videoPath}
		if isTwitterLink(stepParam.Link) {
			cmdArgs = []string{"-f", "bv*+ba/b", "--merge-output-format", "mp4", "--no-warnings", "-o", videoPath}
		}
		if config.Conf.App.Proxy != "" {
			cmdArgs = append(cmdArgs, "--proxy", config.Conf.App.Proxy)
		}
		if isBilibiliLink(stepParam.Link) {
			cmdArgs = append(cmdArgs, bilibiliAnti412Args()...)
		}
		if storage.FfmpegPath != "ffmpeg" {
			cmdArgs = append(cmdArgs, "--ffmpeg-location", storage.FfmpegPath)
		}

		if isBilibiliLink(stepParam.Link) {
			output, err = runYtDlpWithBilibiliFallback(cmdArgs, stepParam.Link, cookiesPath)
		} else {
			args := append([]string{}, cmdArgs...)
			if fileExists(cookiesPath) {
				args = append(args, "--cookies", cookiesPath)
			}
			args = append(args, stepParam.Link)
			cmd := exec.Command(storage.YtdlpPath, args...)
			output, err = cmd.CombinedOutput()
			if err != nil && isRequestedFormatUnavailable(output) {
				fallbackArgs := []string{"-f", "bv*+ba/b", "--merge-output-format", "mp4", "--no-warnings", "-o", videoPath}
				if config.Conf.App.Proxy != "" {
					fallbackArgs = append(fallbackArgs, "--proxy", config.Conf.App.Proxy)
				}
				if storage.FfmpegPath != "ffmpeg" {
					fallbackArgs = append(fallbackArgs, "--ffmpeg-location", storage.FfmpegPath)
				}
				retryArgs := append([]string{}, fallbackArgs...)
				if fileExists(cookiesPath) {
					retryArgs = append(retryArgs, "--cookies", cookiesPath)
				}
				retryArgs = append(retryArgs, stepParam.Link)
				retryCmd := exec.Command(storage.YtdlpPath, retryArgs...)
				retryOutput, retryErr := retryCmd.CombinedOutput()
				if retryErr == nil {
					output = retryOutput
					err = nil
				} else {
					output = append(output, []byte("\n[fallback generic format]\n"+string(retryOutput))...)
					err = retryErr
				}
			}
			if err != nil && isTwitterLink(stepParam.Link) && isTwitterAuthRequired(output) {
				browserFallbackArgs := append([]string{}, cmdArgs...)
				browserFallbackOutput, browserFallbackErr := runYtDlpWithCookiesFromBrowser(browserFallbackArgs, stepParam.Link)
				if browserFallbackErr == nil {
					output = browserFallbackOutput
					err = nil
				} else {
					output = append(output, []byte("\n[fallback cookies-from-browser]\n"+string(browserFallbackOutput))...)
					err = browserFallbackErr
				}
			}
		}

		if err != nil {
			log.GetLogger().Error("linkToFile download video yt-dlp error", zap.Any("step param", stepParam), zap.String("output", string(output)), zap.Error(err))
			if isBilibiliLink(stepParam.Link) {
				errDetail := strings.TrimSpace(string(output))
				if len(errDetail) > 300 {
					errDetail = "... " + errDetail[len(errDetail)-300:]
				}
				hint := bilibiliDownloadHint(output, cookiesPath)
				return fmt.Errorf("B站视频下载失败。%s 详情: %s", hint, errDetail)
			}
			if isTwitterLink(stepParam.Link) {
				hint := twitterDownloadHint(output, cookiesPath)
				return fmt.Errorf("Twitter/X download video failed. %s Details: %s", hint, tailOutput(output, 600))
			}
			return fmt.Errorf("linkToFile download video yt-dlp error: %w", err)
		}
		stepParam.InputVideoPath = videoPath
	}

	stepParam.TaskPtr.ProcessPct = 10
	return nil
}
