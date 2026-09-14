package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"wx_channel/internal/config"
	"wx_channel/internal/response"
	"wx_channel/internal/services"
)

// AuthorBackfillAPI 处理作者全量视频下载相关的 API
type AuthorBackfillAPI struct {
	service *services.AuthorBackfillService
}

// NewAuthorBackfillAPI 创建作者全量下载 API 处理器
func NewAuthorBackfillAPI(service *services.AuthorBackfillService) *AuthorBackfillAPI {
	return &AuthorBackfillAPI{service: service}
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
