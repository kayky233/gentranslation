package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/samber/lo"
	"go.uber.org/zap"
	"krillin-ai/internal/dto"
	"krillin-ai/internal/storage"
	"krillin-ai/internal/types"
	"krillin-ai/log"
	"krillin-ai/pkg/util"
)

func (s Service) StartSubtitleTask(req dto.StartVideoSubtitleTaskReq) (*dto.StartVideoSubtitleTaskResData, error) {
	if isYouTubeLink(req.Url) {
		videoID, _ := util.GetYouTubeID(req.Url)
		if videoID == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}
	if isBilibiliLink(req.Url) {
		videoID := util.GetBilibiliVideoId(req.Url)
		if videoID == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}
	if isTwitterLink(req.Url) {
		statusID := util.GetTwitterStatusID(req.Url)
		if statusID == "" {
			return nil, fmt.Errorf("链接不合法")
		}
	}

	separates := strings.Split(req.Url, "/")
	taskIDSeed := "task"
	if len(separates) > 0 {
		seed := strings.ReplaceAll(separates[len(separates)-1], " ", "")
		runes := []rune(seed)
		if len(runes) > 16 {
			runes = runes[:16]
		}
		if len(runes) > 0 {
			taskIDSeed = string(runes)
		}
	}
	taskID := fmt.Sprintf("%s_%s", util.SanitizePathName(taskIDSeed), util.GenerateRandStringWithUpperLowerNum(4))
	taskID = strings.ReplaceAll(taskID, "=", "")
	taskID = strings.ReplaceAll(taskID, "?", "")

	var resultType types.SubtitleResultType
	if req.TargetLang == "none" {
		resultType = types.SubtitleResultTypeOriginOnly
	} else {
		if req.Bilingual == types.SubtitleTaskBilingualYes {
			if req.TranslationSubtitlePos == types.SubtitleTaskTranslationSubtitlePosTop {
				resultType = types.SubtitleResultTypeBilingualTranslationOnTop
			} else {
				resultType = types.SubtitleResultTypeBilingualTranslationOnBottom
			}
		} else {
			resultType = types.SubtitleResultTypeTargetOnly
		}
	}

	replaceWordsMap := make(map[string]string)
	if len(req.Replace) > 0 {
		for _, replace := range req.Replace {
			beforeAfter := strings.Split(replace, "|")
			if len(beforeAfter) == 2 {
				replaceWordsMap[beforeAfter[0]] = beforeAfter[1]
			} else {
				log.GetLogger().Info("generateAudioSubtitles replace param length err", zap.Any("replace", replace), zap.Any("taskID", taskID))
			}
		}
	}

	var err error
	ctx := context.Background()
	taskBasePath := filepath.Join("./tasks", taskID)
	if _, err = os.Stat(taskBasePath); os.IsNotExist(err) {
		err = os.MkdirAll(filepath.Join(taskBasePath, "output"), os.ModePerm)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask MkdirAll err", zap.Any("req", req), zap.Error(err))
		}
	}

	taskPtr := &types.SubtitleTask{
		TaskId:   taskID,
		VideoSrc: req.Url,
		Status:   types.SubtitleTaskStatusProcessing,
	}
	storage.SubtitleTasks.Store(taskID, taskPtr)

	var voiceCloneAudioURL string
	if req.TtsVoiceCloneSrcFileUrl != "" {
		localFileURL := strings.TrimPrefix(req.TtsVoiceCloneSrcFileUrl, "local:")
		fileKey := util.GenerateRandStringWithUpperLowerNum(5) + filepath.Ext(localFileURL)
		err = s.OssClient.UploadFile(context.Background(), fileKey, localFileURL, s.OssClient.Bucket)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask UploadFile err", zap.Any("req", req), zap.Error(err))
			return nil, errors.New("上传声音克隆源失败")
		}
		voiceCloneAudioURL = fmt.Sprintf("https://%s.oss-cn-shanghai.aliyuncs.com/%s", s.OssClient.Bucket, fileKey)
		log.GetLogger().Info("StartVideoSubtitleTask 上传声音克隆源成功", zap.Any("oss url", voiceCloneAudioURL))
	}

	var cookiesFilePath string
	if req.CookiesFileUrl != "" {
		localCookiesPath := strings.TrimPrefix(req.CookiesFileUrl, "local:")
		if localCookiesPath == req.CookiesFileUrl {
			return nil, errors.New("cookies 文件参数不合法")
		}
		if _, statErr := os.Stat(localCookiesPath); statErr != nil {
			return nil, errors.New("cookies 文件不存在或不可读")
		}
		cookiesFilePath = filepath.Join(taskBasePath, "cookies.txt")
		if err = util.CopyFile(localCookiesPath, cookiesFilePath); err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask copy cookies file err", zap.Any("req", req), zap.Error(err))
			return nil, errors.New("复制 cookies 文件失败")
		}
	} else {
		globalCookiesPath := resolveCookiesPath(nil)
		if fileExists(globalCookiesPath) {
			cookiesFilePath = filepath.Join(taskBasePath, "cookies.txt")
			if err = util.CopyFile(globalCookiesPath, cookiesFilePath); err != nil {
				log.GetLogger().Warn("StartVideoSubtitleTask copy global cookies file err, fallback to original path", zap.String("globalCookiesPath", globalCookiesPath), zap.Error(err))
				cookiesFilePath = globalCookiesPath
			}
		}
	}

	stepParam := types.SubtitleTaskStepParam{
		TaskId:                  taskID,
		TaskPtr:                 taskPtr,
		TaskBasePath:            taskBasePath,
		Link:                    req.Url,
		CookiesFilePath:         cookiesFilePath,
		SubtitleResultType:      resultType,
		EnableModalFilter:       req.ModalFilter == types.SubtitleTaskModalFilterYes,
		EnableTts:               req.Tts == types.SubtitleTaskTtsYes,
		TtsVoiceCode:            req.TtsVoiceCode,
		VoiceCloneAudioUrl:      voiceCloneAudioURL,
		ReplaceWordsMap:         replaceWordsMap,
		OriginLanguage:          types.StandardLanguageCode(req.OriginLanguage),
		TargetLanguage:          types.StandardLanguageCode(req.TargetLang),
		UserUILanguage:          types.StandardLanguageCode(req.Language),
		EmbedSubtitleVideoType:  req.EmbedSubtitleVideoType,
		VerticalVideoMajorTitle: req.VerticalMajorTitle,
		VerticalVideoMinorTitle: req.VerticalMinorTitle,
		MaxWordOneLine:          12,
	}
	if req.OriginLanguageWordOneLine != 0 {
		stepParam.MaxWordOneLine = req.OriginLanguageWordOneLine
	}

	log.GetLogger().Info("current task info", zap.String("taskID", taskID), zap.Any("param", stepParam))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				log.GetLogger().Error("autoVideoSubtitle panic", zap.Any("panic:", r), zap.Any("stack:", buf))
				stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			}
		}()

		log.GetLogger().Info("video subtitle start task", zap.String("taskID", taskID))
		err = s.linkToFile(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask linkToFile err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}

		err = s.audioToSubtitle(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask audioToSubtitle err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}
		err = s.srtFileToSpeech(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask srtFileToSpeech err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}
		err = s.embedSubtitles(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask embedSubtitles err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}
		err = s.uploadSubtitles(ctx, &stepParam)
		if err != nil {
			log.GetLogger().Error("StartVideoSubtitleTask uploadSubtitles err", zap.Any("req", req), zap.Error(err))
			stepParam.TaskPtr.Status = types.SubtitleTaskStatusFailed
			stepParam.TaskPtr.FailReason = err.Error()
			return
		}

		log.GetLogger().Info("video subtitle task end", zap.String("taskID", taskID))
	}()

	return &dto.StartVideoSubtitleTaskResData{TaskId: taskID}, nil
}

func (s Service) GetTaskStatus(req dto.GetVideoSubtitleTaskReq) (*dto.GetVideoSubtitleTaskResData, error) {
	task, ok := storage.SubtitleTasks.Load(req.TaskId)
	if !ok || task == nil {
		return nil, errors.New("任务不存在")
	}
	taskPtr := task.(*types.SubtitleTask)
	if taskPtr.Status == types.SubtitleTaskStatusFailed {
		return nil, fmt.Errorf("任务失败，原因：%s", taskPtr.FailReason)
	}
	return &dto.GetVideoSubtitleTaskResData{
		TaskId:         taskPtr.TaskId,
		ProcessPercent: taskPtr.ProcessPct,
		VideoInfo: &dto.VideoInfo{
			Title:                 taskPtr.Title,
			Description:           taskPtr.Description,
			TranslatedTitle:       taskPtr.TranslatedTitle,
			TranslatedDescription: taskPtr.TranslatedDescription,
		},
		SubtitleInfo: lo.Map(taskPtr.SubtitleInfos, func(item types.SubtitleInfo, _ int) *dto.SubtitleInfo {
			return &dto.SubtitleInfo{
				Name:        item.Name,
				DownloadUrl: item.DownloadUrl,
			}
		}),
		TargetLanguage:    taskPtr.TargetLanguage,
		SpeechDownloadUrl: taskPtr.SpeechDownloadUrl,
	}, nil
}
