package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"wx_channel/internal/database"
	"wx_channel/internal/utils"
	"wx_channel/internal/websocket"
)

// RadarService 负责定时轮询并下载对标账号的新视频
// Requirements: Competitor 24-hour Silent Radar
type RadarService struct {
	repo         *database.RadarRepository
	queueService *QueueService
	hub          *websocket.Hub
	settings     *database.SettingsRepository

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	wg     sync.WaitGroup

	ticker *time.Ticker
}

// NewRadarService 创建一个新的雷达服务
func NewRadarService(repo *database.RadarRepository, queueService *QueueService, hub *websocket.Hub) *RadarService {
	ctx, cancel := context.WithCancel(context.Background())
	return &RadarService{
		repo:         repo,
		queueService: queueService,
		hub:          hub,
		settings:     database.NewSettingsRepository(),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start 启动雷达服务轮询器
func (s *RadarService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ticker != nil {
		return // 已启动
	}
	if s.ctx == nil || s.ctx.Err() != nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}

	// 默认每分钟检查一次，但实际是否触发取决于每个 target 的 interval_minutes
	s.ticker = time.NewTicker(time.Minute)
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		utils.LogInfo("Radar Service (24h静默雷达) 已启动")

		// 启动时立即执行一次检测（延迟10秒，等待WebSocket连接建立）
		time.Sleep(10 * time.Second)
		s.checkTargets()

		for {
			select {
			case <-s.ctx.Done():
				utils.LogInfo("Radar Service 已停止")
				return
			case <-s.ticker.C:
				s.checkTargets()
			}
		}
	}()
}

// Stop 停止雷达服务
func (s *RadarService) Stop() {
	s.mu.Lock()
	if s.ticker == nil {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	ticker := s.ticker
	if s.ticker != nil {
		s.ticker = nil
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ticker != nil {
		ticker.Stop()
	}
	s.wg.Wait()
}

// checkTargets 遍历并检查所有活动的雷达目标
func (s *RadarService) checkTargets() {
	targets, err := s.repo.GetActive()
	if err != nil {
		utils.LogError("获取活动雷达目标失败: %v", err)
		return
	}

	if len(targets) == 0 {
		return
	}

	now := time.Now()
	hasClient := s.hub.ClientCount() > 0

	for _, target := range targets {
		// 检查是否到了该检测的时间
		if target.LastCheckTime != nil {
			elapsed := now.Sub(*target.LastCheckTime)
			if elapsed < time.Duration(target.IntervalMinutes)*time.Minute {
				continue // 还没到时间
			}
		}

		if !hasClient {
			// 更新最后检测时间
			_ = s.repo.UpdateLastCheckTime(target.ID, now)
			// 插入错误日志
			_ = s.repo.AddLog(&database.RadarLog{
				TargetID:     target.ID,
				CheckTime:    now,
				Status:       "error",
				ErrorMessage: "微信客户端未连接或被关闭，请重新注入",
			})
			continue // 跳过实际检测
		}

		// 执行检测
		s.processTarget(target)
	}
}

// CheckNow 手动触发指定目标的立即检测（不受轮询间隔限制）
func (s *RadarService) CheckNow(targetID string) (*database.RadarLog, error) {
	target, err := s.repo.GetByID(targetID)
	if err != nil {
		return nil, fmt.Errorf("读取监控目标失败: %w", err)
	}
	if target == nil {
		return nil, fmt.Errorf("监控目标不存在: %s", targetID)
	}
	if s.hub == nil || s.hub.ClientCount() == 0 {
		now := time.Now()
		_ = s.repo.UpdateLastCheckTime(target.ID, now)
		_ = s.repo.AddLog(&database.RadarLog{
			TargetID:     target.ID,
			CheckTime:    now,
			Status:       "error",
			ErrorMessage: "微信客户端未连接或被关闭，请重新注入",
		})
		return nil, fmt.Errorf("微信客户端未连接或被关闭，请重新注入")
	}

	s.processTarget(*target)

	logs, err := s.repo.GetLogsByTargetID(target.ID, 1)
	if err != nil || len(logs) == 0 {
		return nil, nil
	}
	return &logs[0], nil
}

// processTarget 处理单个雷达监控目标的拉取与对比逻辑
func (s *RadarService) processTarget(target database.RadarTarget) {
	utils.LogInfo("[Radar] 开始检测账号: %s (%s)", target.AuthorName, target.Username)

	// 更新最后检测时间
	now := time.Now()
	if err := s.repo.UpdateLastCheckTime(target.ID, now); err != nil {
		utils.LogError("[Radar] 更新检测时间失败 [%s]: %v", target.ID, err)
	}

	// 初始化日志记录
	radarLog := &database.RadarLog{
		TargetID:  target.ID,
		CheckTime: now,
		Status:    "success",
	}

	// 1. 调用 WebSocket 获取用户视频列表 (feed_list)
	body := websocket.FeedListBody{
		Username:   target.Username,
		NextMarker: "", // 只需要第一页最新数据
	}

	// 限制 30 秒超时
	data, err := s.hub.CallAPI("key:channels:feed_list", body, 30*time.Second)
	if err != nil {
		radarLog.Status = "error"
		if strings.Contains(err.Error(), "no available client") {
			radarLog.ErrorMessage = "微信客户端未连接或已退出"
			utils.LogWarn("[Radar] 检测失败 [%s]: %s", target.AuthorName, radarLog.ErrorMessage)
		} else {
			radarLog.ErrorMessage = err.Error()
			utils.LogError("[Radar] 获取视频列表失败 [%s]: %v", target.AuthorName, err)
		}
		_ = s.repo.AddLog(radarLog)
		return
	}

	// 2. 解析返回列表数据
	videos, _, ret, parseErr := ParseFeedListResponse(data)
	if parseErr != nil {
		radarLog.Status = "error"
		radarLog.ErrorMessage = "解析返回数据失败: " + parseErr.Error()
		utils.LogError("[Radar] 解析视频列表失败 [%s]: %v", target.AuthorName, parseErr)
		_ = s.repo.AddLog(radarLog)
		return
	}

	if ret != 0 {
		radarLog.Status = "error"
		radarLog.ErrorMessage = fmt.Sprintf("微信接口返回失败，状态码: %d (可能是请求过于频繁或账号异常)", ret)
		utils.LogWarn("[Radar] 账号 [%s] 获取数据被微信拒绝(Ret:%d)", target.AuthorName, ret)
		_ = s.repo.AddLog(radarLog)
		return
	}

	radarLog.FoundVideos = len(videos)

	if radarLog.FoundVideos == 0 {
		utils.LogInfo("[Radar] 账号 [%s] 暂无视频数据(Raw Data Size: %d)", target.AuthorName, len(data))
		_ = s.repo.AddLog(radarLog)
		return
	}

	// 3. 检查库中是否已存在并提取新视频
	newVideoCount := 0
	settings, _ := s.settings.Load()
	if settings == nil {
		settings = database.DefaultSettings()
	}

	// 用于记录本次扫描的所有视频摘要
	var videoSummaries []database.RadarVideoSummary

	for _, video := range videos {
		isNew := !videoAlreadyHandled(database.NewDownloadRecordRepository(), s.queueService, video.VideoID)

		// 记录视频摘要
		videoSummaries = append(videoSummaries, database.RadarVideoSummary{
			VideoID: video.VideoID,
			Title:   video.Title,
			IsNew:   isNew,
		})

		if isNew {
			title := video.Title
			if title == "" {
				title = fmt.Sprintf("RadarV_%s", video.VideoID)
			}
			if video.VideoURL == "" {
				utils.LogWarn("[Radar] 新视频 [%s] 无法提取 URL，跳过: %s", target.AuthorName, video.VideoID)
				continue
			}
			utils.LogInfo("[Radar] 发现新视频 [%s]: %s (%s)", target.AuthorName, title, video.VideoID)
			newVideoCount++

			// 直接从 feed_list 数据入队，无需额外请求 feed_profile
			req := []VideoInfo{{
				VideoID:    video.VideoID,
				Title:      title,
				Author:     target.AuthorName,
				VideoURL:   video.VideoURL,
				CoverURL:   video.CoverURL,
				Size:       video.Size,
				DecryptKey: video.DecryptKey,
				Duration:   video.Duration,
				Resolution: video.Resolution,
			}}
			if _, err := s.queueService.AddToQueue(req); err != nil {
				utils.LogError("[Radar] 添加视频到下载队列失败 [%s]-[%s]: %v", target.AuthorName, title, err)
			} else {
				utils.LogInfo("[Radar] 成功加入队列: %s", title)
			}
		}
	}

	radarLog.NewVideos = newVideoCount

	// 将视频摘要序列化后存入日志
	if len(videoSummaries) > 0 {
		if b, err := json.Marshal(videoSummaries); err == nil {
			radarLog.VideoList = string(b)
		}
	}

	_ = s.repo.AddLog(radarLog)

	if newVideoCount > 0 {
		utils.LogInfo("[Radar] 账号 [%s] 检测完毕，新增 %d 个视频并加入下载队列", target.AuthorName, newVideoCount)
	}
}
