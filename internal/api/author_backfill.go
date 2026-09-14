package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"wx_channel/internal/config"
	"wx_channel/internal/database"
	"wx_channel/internal/response"
	"wx_channel/internal/services"
	"wx_channel/internal/websocket"
)

// AuthorBackfillAPI 处理作者全量视频下载相关的 API
type AuthorBackfillAPI struct {
	service *services.AuthorBackfillService
	hub     *websocket.Hub
}

// NewAuthorBackfillAPI 创建作者全量下载 API 处理器
func NewAuthorBackfillAPI(service *services.AuthorBackfillService, hub *websocket.Hub) *AuthorBackfillAPI {
	return &AuthorBackfillAPI{service: service, hub: hub}
}

type backfillStartRequest struct {
	Username   string `json:"username"`
	AuthorName string `json:"author_name"`
}

// Start 启动指定作者的全量下载任务
func (h *AuthorBackfillAPI) Start(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.Error(w, http.StatusServiceUnavailable, "全量下载服务未初始化")
		return
	}

	var req backfillStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求参数解析失败")
		return
	}

	job, err := h.service.Start(req.Username, req.AuthorName)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(w, job)
}

// Status 查询全量下载任务状态（username 为空时返回全部任务）
func (h *AuthorBackfillAPI) Status(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.Error(w, http.StatusServiceUnavailable, "全量下载服务未初始化")
		return
	}

	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username != "" {
		job, ok := h.service.GetJob(username)
		if !ok {
			response.Error(w, http.StatusNotFound, "未找到该作者的任务")
			return
		}
		response.Success(w, job)
		return
	}

	response.Success(w, h.service.ListJobs())
}

// Stop 停止指定作者的全量下载任务
func (h *AuthorBackfillAPI) Stop(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		response.Error(w, http.StatusServiceUnavailable, "全量下载服务未初始化")
		return
	}

	var req backfillStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求参数解析失败")
		return
	}

	if !h.service.Stop(req.Username) {
		response.Error(w, http.StatusNotFound, "该作者没有进行中的任务")
		return
	}

	response.Success(w, nil)
}

// RecoverKeys 为缺解密密钥的队列项补取密钥（手动触发）。
// 按每个队列项的 VideoID 调视频详情接口（feed_profile），解析 decodeKey 后更新回队列。
func (h *AuthorBackfillAPI) RecoverKeys(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil || h.hub.ClientCount() == 0 {
		response.Error(w, http.StatusServiceUnavailable, "微信客户端未连接或已退出，请先打开一个视频号页面后重试")
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求参数解析失败")
		return
	}

	queueRepo := database.NewQueueRepository()
	var targets []*database.QueueItem
	if len(req.IDs) > 0 {
		for _, id := range req.IDs {
			item, err := queueRepo.GetByID(id)
			if err != nil || item == nil {
				continue
			}
			targets = append(targets, item)
		}
	} else {
		items, err := queueRepo.List()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "读取队列失败: "+err.Error())
			return
		}
		for i := range items {
			if items[i].DecryptKey == "" && items[i].VideoURL != "" {
				targets = append(targets, &items[i])
			}
		}
	}

	if len(targets) == 0 {
		response.Success(w, map[string]int{"recovered": 0, "failed": 0, "scanned": 0})
		return
	}

	recovered, failed := 0, 0
	for _, item := range targets {
		if item.DecryptKey != "" {
			continue // 已有密钥，跳过
		}
		if item.VideoID == "" {
			failed++
			continue
		}

		body := websocket.FeedProfileBody{ObjectID: item.VideoID}
		data, err := h.hub.CallAPI("key:channels:feed_profile", body, 30*time.Second)
		if err != nil {
			failed++
			continue
		}

		key, err := extractDecryptKeyFromProfile(data)
		if err != nil || key == "" {
			failed++
			continue
		}

		item.DecryptKey = key
		if err := queueRepo.Update(item); err != nil {
			failed++
			continue
		}
		recovered++
	}

	response.Success(w, map[string]int{
		"recovered": recovered,
		"failed":    failed,
		"scanned":   len(targets),
	})
}

// extractDecryptKeyFromProfile 从视频详情接口的返回中提取解密密钥
func extractDecryptKeyFromProfile(data []byte) (string, error) {
	var raw struct {
		Data map[string]interface{} `json:"data"`
	}
	// 兼容直接返回 data 对象或包一层 data 的情况
	var direct map[string]interface{}
	if err := json.Unmarshal(data, &direct); err == nil {
		if inner, ok := direct["data"].(map[string]interface{}); ok {
			raw.Data = inner
		} else {
			raw.Data = direct
		}
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return "", err
		}
	}

	obj := raw.Data["object"]
	desc := findMapByKey(obj, "objectDesc")
	if desc == nil {
		desc = findMapByKey(raw.Data, "objectDesc")
	}
	if desc == nil {
		return "", fmt.Errorf("未找到 objectDesc")
	}
	mediaList, _ := desc["media"].([]interface{})
	if len(mediaList) == 0 {
		return "", fmt.Errorf("未找到 media")
	}
	media, ok := mediaList[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("media 格式异常")
	}
	switch k := media["decodeKey"].(type) {
	case string:
		return k, nil
	case float64:
		return fmt.Sprintf("%.0f", k), nil
	}
	return "", nil
}

// findMapByKey 在任意嵌套结构中递归查找键对应的 map（防御性解析，兼容不同返回层级）
func findMapByKey(node interface{}, key string) map[string]interface{} {
	switch v := node.(type) {
	case map[string]interface{}:
		if m, ok := v[key].(map[string]interface{}); ok {
			return m
		}
		for _, child := range v {
			if found := findMapByKey(child, key); found != nil {
				return found
			}
		}
	case []interface{}:
		for _, item := range v {
			if found := findMapByKey(item, key); found != nil {
				return found
			}
		}
	}
	return nil
}

// SettingsGet 获取作者全量下载的运行时配置（翻页上限、间隔）
func (h *AuthorBackfillAPI) SettingsGet(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	response.Success(w, map[string]int{
		"max_pages":   cfg.AuthorBackfillMaxPages,
		"page_delay":  cfg.AuthorBackfillPageDelay,
	})
}

// SettingsUpdate 更新作者全量下载的运行时配置（立即生效，不影响进行中的任务）
func (h *AuthorBackfillAPI) SettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxPages  *int `json:"max_pages"`
		PageDelay *int `json:"page_delay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求参数解析失败")
		return
	}

	cfg := config.Get()
	if req.MaxPages != nil {
		if *req.MaxPages < 1 || *req.MaxPages > 10000 {
			response.Error(w, http.StatusBadRequest, "翻页上限需在 1-10000 之间")
			return
		}
		cfg.AuthorBackfillMaxPages = *req.MaxPages
	}
	if req.PageDelay != nil {
		if *req.PageDelay < 1 || *req.PageDelay > 600 {
			response.Error(w, http.StatusBadRequest, "翻页间隔需在 1-600 秒之间")
			return
		}
		cfg.AuthorBackfillPageDelay = *req.PageDelay
	}

	response.Success(w, map[string]int{
		"max_pages":  cfg.AuthorBackfillMaxPages,
		"page_delay": cfg.AuthorBackfillPageDelay,
	})
}

// RegisterRoutes 注册作者全量下载相关的 API 路由
func (h *AuthorBackfillAPI) RegisterRoutes(mux *http.ServeMux) {
	for _, prefix := range []string{"/api/author/backfill", "/api/v1/author/backfill"} {
		mux.HandleFunc(prefix+"/start", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				response.Error(w, http.StatusMethodNotAllowed, "不允许的请求方法")
				return
			}
			h.Start(w, r)
		})
		mux.HandleFunc(prefix+"/status", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				response.Error(w, http.StatusMethodNotAllowed, "不允许的请求方法")
				return
			}
			h.Status(w, r)
		})
		mux.HandleFunc(prefix+"/stop", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				response.Error(w, http.StatusMethodNotAllowed, "不允许的请求方法")
				return
			}
			h.Stop(w, r)
		})
		mux.HandleFunc(prefix+"/recover_keys", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				response.Error(w, http.StatusMethodNotAllowed, "不允许的请求方法")
				return
			}
			h.RecoverKeys(w, r)
		})
		mux.HandleFunc(prefix+"/settings", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				h.SettingsGet(w, r)
			case http.MethodPut, http.MethodPost:
				h.SettingsUpdate(w, r)
			default:
				response.Error(w, http.StatusMethodNotAllowed, "不允许的请求方法")
			}
		})
	}
}
