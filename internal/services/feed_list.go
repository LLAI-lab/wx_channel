package services

import (
	"encoding/json"
	"fmt"

	"wx_channel/internal/database"
)

// FeedListVideo 表示账号视频列表接口返回的单个视频条目
type FeedListVideo struct {
	VideoID    string
	Title      string
	VideoURL   string
	CoverURL   string
	DecryptKey string
	Size       int64
	Duration   int64
	Resolution string
}

// feedListResponse 与视频号网页端 finderUserPage 返回结构对应
type feedListResponse struct {
	Data struct {
		BaseResponse struct {
			Ret int `json:"Ret"`
		} `json:"BaseResponse"`
		ObjectList []interface{} `json:"objectList"`
		Object     []interface{} `json:"object"`
		LastBuffer string        `json:"lastBuffer"`
	} `json:"data"`
}

// ParseFeedListResponse 解析 key:channels:feed_list 的原始返回。
// 兼容新老版本微信返回的 objectList / object 字段，并提取 lastBuffer 翻页游标。
func ParseFeedListResponse(data []byte) (videos []FeedListVideo, lastBuffer string, ret int, err error) {
	var raw feedListResponse
	if err = json.Unmarshal(data, &raw); err != nil {
		return nil, "", 0, err
	}

	allObjects := raw.Data.ObjectList
	if len(allObjects) == 0 && len(raw.Data.Object) > 0 {
		allObjects = raw.Data.Object
	}

	videos = make([]FeedListVideo, 0, len(allObjects))
	for _, objInter := range allObjects {
		objMap, ok := objInter.(map[string]interface{})
		if !ok {
			continue
		}

		idInter, ok := objMap["id"]
		if !ok || idInter == "" {
			continue
		}
		video := FeedListVideo{VideoID: fmt.Sprintf("%v", idInter)}

		if descInter, ok := objMap["objectDesc"]; ok {
			if descMap, ok := descInter.(map[string]interface{}); ok {
				video.Title, _ = descMap["description"].(string)

				if mediaList, ok := descMap["media"].([]interface{}); ok && len(mediaList) > 0 {
					if m, ok := mediaList[0].(map[string]interface{}); ok {
						rawURL, _ := m["url"].(string)
						urlToken, _ := m["urlToken"].(string)
						if rawURL != "" {
							video.VideoURL = rawURL + urlToken
						}
						video.CoverURL, _ = m["thumbUrl"].(string)
						video.DecryptKey, _ = m["decodeKey"].(string)
						if fs, ok := m["fileSize"].(float64); ok {
							video.Size = int64(fs)
						}
						if dur, ok := m["videoDuration"].(float64); ok {
							video.Duration = int64(dur)
						}
						video.Resolution, _ = m["videoResolution"].(string)
					}
				}
			}
		}

		videos = append(videos, video)
	}

	return videos, raw.Data.LastBuffer, raw.Data.BaseResponse.Ret, nil
}

// FilterQueuedVideos 过滤掉已完成下载或已在队列中的视频，返回待下载列表与跳过数量
func (s *QueueService) FilterQueuedVideos(videos []FeedListVideo) (pending []FeedListVideo, skipped int) {
	downloadRepo := database.NewDownloadRecordRepository()
	pending = make([]FeedListVideo, 0, len(videos))
	for _, video := range videos {
		if videoAlreadyHandled(downloadRepo, s, video.VideoID) {
			skipped++
			continue
		}
		pending = append(pending, video)
	}
	return pending, skipped
}

// videoAlreadyHandled 判断视频是否已有下载记录或已存在于队列中
func videoAlreadyHandled(downloadRepo *database.DownloadRecordRepository, queueService *QueueService, videoID string) bool {
	record, _ := downloadRepo.GetByVideoID(videoID)
	if record != nil && (record.Status == database.DownloadStatusCompleted || record.Status == database.DownloadStatusInProgress) {
		return true
	}

	queueItem, _ := queueService.GetByVideoID(videoID)
	if queueItem != nil && (queueItem.Status == database.QueueStatusPending || queueItem.Status == database.QueueStatusDownloading || queueItem.Status == database.QueueStatusCompleted) {
		return true
	}

	return false
}
