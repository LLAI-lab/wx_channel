package app

const releaseHighlightsVersion = "5.8.6"

var releaseHighlights = [...]string{
	"页面 OOM 根治 - 修复实时进度推送触发的 DOM 重建风暴，下载不再撑爆页面",
	"进度广播节流 - 分片下载进度至少间隔 1 秒推送，消除高频广播",
	"完成检测批量化 - 队列完成状态改为一次批量对比，消除 2000+ 逐条请求",
	"内存优化 - GC 收紧加 256MB 软上限，常驻内存显著降低",
	"队列分页渲染 - 长队列每页 200 条加载更多，大队列不再卡顿",
}
