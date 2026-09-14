package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"wx_channel/internal/config"
	"wx_channel/internal/utils"
	"wx_channel/internal/websocket"
)

// AuthorBackfillService 负责翻页拉取指定作者的全部历史视频并加入下载队列
// Requirements: 作者主页一键全量下载，复用 key:channels:feed_list 分页能力
type AuthorBackfillService struct {
	hub          *websocket.Hub
	queueService *QueueService

	mu   sync.Mutex
	jobs map[string]*backfillJobHandle
}

const (
	backfillMaxVideos      = 50000                  // 单次任务视频总量上限
	backfillCallTimeout    = 60 * time.Second       // 单次 feed_list 请求超时
	backfillMaxRetries     = 3                      // 单页请求失败重试次数
	backfillRetryBaseDelay = 3 * time.Second        // 重试退避基数（第n次重试等待 n*基数）
)

// BackfillJobStatus 全量下载任务状态
type BackfillJobStatus string

const (
	BackfillStatusRunning   BackfillJobStatus = "running"
	BackfillStatusCompleted BackfillJobStatus = "completed"
	BackfillStatusFailed    BackfillJobStatus = "failed"
	BackfillStatusStopped   BackfillJobStatus = "stopped"
)

// BackfillJob 单个作者的全量下载任务进度快照（JSON 序列化安全，不含内部句柄）
type BackfillJob struct {
	Username      string            `json:"username"`
	AuthorName    string            `json:"author_name"`
	Status        BackfillJobStatus `json:"status"`
	PagesFetched  int               `json:"pages_fetched"`
	FoundVideos   int               `json:"found_videos"`
	NewVideos     int               `json:"new_videos"`
	SkippedVideos int               `json:"skipped_videos"`
	LastError     string            `json:"last_error,omitempty"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    *time.Time        `json:"finished_at,omitempty"`
}

// backfillJobHandle 保存任务的停止通道，避免 chan 进入 JSON 序列化
type backfillJobHandle struct {
	job      BackfillJob
	stopCh   chan struct{}
	stopOnce sync.Once
}

var errBackfillStopped = errors.New("任务已停止")

// NewAuthorBackfillService 创建作者全量下载服务
func NewAuthorBackfillService(hub *websocket.Hub, queueService *QueueService) *AuthorBackfillService {
	return &AuthorBackfillService{
		hub:          hub,
		queueService: queueService,
		jobs:         make(map[string]*backfillJobHandle),
	}
}

// pageDelay 翻页间隔，可经配置 author_backfill_page_delay 调整（秒），最小1秒防风控
func (s *AuthorBackfillService) pageDelay() time.Duration {
	delay := config.Get().AuthorBackfillPageDelay
	if delay < 1 {
		delay = 1
	}
	return time.Duration(delay) * time.Second
}

// maxPages 单次任务翻页上限，可经配置 author_backfill_max_pages 调整，范围 1-10000
func (s *AuthorBackfillService) maxPages() int {
	maxPages := config.Get().AuthorBackfillMaxPages
	if maxPages < 1 {
		maxPages = 1
	}
	if maxPages > 10000 {
		maxPages = 10000
	}
	return maxPages
}

// Start 启动（或复用）指定作者的全量下载任务
func (s *AuthorBackfillService) Start(username, authorName string) (BackfillJob, error) {
	username = strings.TrimSpace(username)
	authorName = strings.TrimSpace(authorName)
	if username == "" {
		return BackfillJob{}, errors.New("username is required")
	}
	if s.hub == nil || s.hub.ClientCount() == 0 {
		return BackfillJob{}, errors.New("微信客户端未连接或已退出，请先打开一个视频号页面后重试")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if handle, ok := s.jobs[username]; ok && handle.job.Status == BackfillStatusRunning {
		return handle.job, nil // 同一作者任务进行中，直接复用
	}

	handle := &backfillJobHandle{
		job: BackfillJob{
			Username:   username,
			AuthorName: authorName,
			Status:     BackfillStatusRunning,
			StartedAt:  time.Now(),
		},
		stopCh: make(chan struct{}),
	}
	s.jobs[username] = handle

	go s.run(handle)

	utils.LogInfo("[Backfill] 启动作者全量下载: %s (%s)", authorName, username)
	return handle.job, nil
}

// Stop 停止指定作者的全量下载任务
func (s *AuthorBackfillService) Stop(username string) bool {
	username = strings.TrimSpace(username)
	s.mu.Lock()
	handle, ok := s.jobs[username]
	running := ok && handle.job.Status == BackfillStatusRunning
	s.mu.Unlock()

	if !running {
		return false
	}
	handle.stopOnce.Do(func() { close(handle.stopCh) })
	return true
}

// GetJob 获取指定作者的任务快照
func (s *AuthorBackfillService) GetJob(username string) (BackfillJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	handle, ok := s.jobs[strings.TrimSpace(username)]
	if !ok {
		return BackfillJob{}, false
	}
	return handle.job, true
}

// ListJobs 返回全部任务快照（按启动时间倒序）
func (s *AuthorBackfillService) ListJobs() []BackfillJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs := make([]BackfillJob, 0, len(s.jobs))
	for _, handle := range s.jobs {
		jobs = append(jobs, handle.job)
	}
	for i := 1; i < len(jobs); i++ {
		for j := i; j > 0 && jobs[j].StartedAt.After(jobs[j-1].StartedAt); j-- {
			jobs[j], jobs[j-1] = jobs[j-1], jobs[j]
		}
	}
	return jobs
}

// run 执行翻页下载主循环，仅在独立 goroutine 中运行
func (s *AuthorBackfillService) run(handle *backfillJobHandle) {
	pageDelay := s.pageDelay()
	marker := ""

	for {
		select {
		case <-handle.stopCh:
			s.finishJob(handle, BackfillStatusStopped, "已手动停止")
			return
		default:
		}

		if handle.job.PagesFetched >= s.maxPages() {
			s.finishJob(handle, BackfillStatusFailed, fmt.Sprintf("已达单次任务翻页上限(%d页)，如未拉取完成可再次执行", s.maxPages()))
			return
		}
		if handle.job.FoundVideos >= backfillMaxVideos {
			s.finishJob(handle, BackfillStatusFailed, fmt.Sprintf("已达单次任务视频上限(%d个)", backfillMaxVideos))
			return
		}

		videos, lastBuffer, err := s.fetchPage(handle, marker)
		if err != nil {
			if errors.Is(err, errBackfillStopped) {
				s.finishJob(handle, BackfillStatusStopped, "已手动停止")
				return
			}
			s.finishJob(handle, BackfillStatusFailed, err.Error())
			return
		}

		s.mu.Lock()
		handle.job.PagesFetched++
		handle.job.FoundVideos += len(videos)
		s.mu.Unlock()

		s.enqueueVideos(handle, videos)

		// lastBuffer 为空表示已翻到最后一页
		if strings.TrimSpace(lastBuffer) == "" {
			s.finishJob(handle, BackfillStatusCompleted, "")
			return
		}
		marker = lastBuffer

		select {
		case <-handle.stopCh:
			s.finishJob(handle, BackfillStatusStopped, "已手动停止")
			return
		case <-time.After(pageDelay):
		}
	}
}

// fetchPage 拉取单页作者视频列表，对传输错误/解析错误/微信 Ret 拒绝做退避重试
func (s *AuthorBackfillService) fetchPage(handle *backfillJobHandle, marker string) ([]FeedListVideo, string, error) {
	var lastErr error

	for attempt := 0; attempt < backfillMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-handle.stopCh:
				return nil, "", errBackfillStopped
			case <-time.After(backfillRetryBaseDelay * time.Duration(attempt)):
			}
		}

		body := websocket.FeedListBody{
			Username:   handle.job.Username,
			NextMarker: marker,
		}
		data, err := s.hub.CallAPI("key:channels:feed_list", body, backfillCallTimeout)
		if err != nil {
			lastErr = err
			utils.LogWarn("[Backfill] [%s] 第%d次请求失败: %v", handle.job.Username, attempt+1, err)
			continue
		}

		videos, lastBuffer, ret, err := ParseFeedListResponse(data)
		if err != nil {
			lastErr = fmt.Errorf("解析返回数据失败: %w", err)
			utils.LogWarn("[Backfill] [%s] 第%d次解析失败: %v", handle.job.Username, attempt+1, err)
			continue
		}
		if ret != 0 {
			lastErr = fmt.Errorf("微信接口返回失败(Ret:%d)，可能请求过于频繁或账号异常", ret)
			utils.LogWarn("[Backfill] [%s] 第%d次被微信拒绝(Ret:%d)", handle.job.Username, attempt+1, ret)
			continue
		}
		return videos, lastBuffer, nil
	}

	return nil, "", lastErr
}

// enqueueVideos 将页内新视频去重后加入下载队列
func (s *AuthorBackfillService) enqueueVideos(handle *backfillJobHandle, videos []FeedListVideo) {
	if len(videos) == 0 {
		return
	}

	pending, skipped := s.queueService.FilterQueuedVideos(videos)
	s.mu.Lock()
	handle.job.SkippedVideos += skipped
	s.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	req := make([]VideoInfo, 0, len(pending))
	for _, video := range pending {
		if video.VideoURL == "" {
			utils.LogWarn("[Backfill] [%s] 视频无法提取 URL，跳过: %s", handle.job.AuthorName, video.VideoID)
			continue
		}
		title := video.Title
		if title == "" {
			title = fmt.Sprintf("%s_%s", handle.job.AuthorName, video.VideoID)
		}
		req = append(req, VideoInfo{
			VideoID:    video.VideoID,
			Title:      title,
			Author:     handle.job.AuthorName,
			VideoURL:   video.VideoURL,
			CoverURL:   video.CoverURL,
			Size:       video.Size,
			DecryptKey: video.DecryptKey,
			Duration:   video.Duration,
			Resolution: video.Resolution,
		})
	}

	if len(req) == 0 {
		return
	}

	added, err := s.queueService.AddToQueue(req)
	if err != nil {
		utils.LogError("[Backfill] [%s] 加入下载队列失败: %v", handle.job.AuthorName, err)
		return
	}

	s.mu.Lock()
	handle.job.NewVideos += len(added)
	s.mu.Unlock()
	utils.LogInfo("[Backfill] [%s] 本页新增 %d 个视频加入队列（发现%d，跳过已有%d）",
		handle.job.AuthorName, len(added), len(videos), skipped)
}

// finishJob 结束任务并记录摘要
func (s *AuthorBackfillService) finishJob(handle *backfillJobHandle, status BackfillJobStatus, errMsg string) {
	s.mu.Lock()
	handle.job.Status = status
	handle.job.LastError = errMsg
	now := time.Now()
	handle.job.FinishedAt = &now
	s.mu.Unlock()

	utils.LogInfo("[Backfill] [%s] 任务结束(%s): 翻页%d，发现%d个视频，新增入队%d，跳过已有%d %s",
		handle.job.AuthorName, status, handle.job.PagesFetched, handle.job.FoundVideos,
		handle.job.NewVideos, handle.job.SkippedVideos, errMsg)
}
