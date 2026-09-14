package app

const releaseHighlightsVersion = "5.8.5"

var releaseHighlights = [...]string{
	"页面 OOM 根治 - 修复实时进度推送触发的 DOM 重建风暴，下载不再撑爆页面",
	"进度广播节流 - 分片下载进度至少间隔 1 秒推送，消除高频广播",
	"轻量进度更新 - 进度条原地更新不再重建节点，页面内存平稳",
	"内存优化 - GC 收紧加 256MB 软上限，常驻内存显著降低",
	"队列分页渲染 - 长队列每页 200 条加载更多，大队列不再卡顿",
}
