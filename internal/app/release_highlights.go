package app

const releaseHighlightsVersion = "5.8.4"

var releaseHighlights = [...]string{
	"内存优化 - GC 收紧加 256MB 软上限，常驻内存显著降低",
	"页面防溢出 - 修复下载大量视频时队列页 Out of Memory，批量进度只渲染进行中任务",
	"队列分页渲染 - 长队列每页 200 条加载更多，大队列不再卡顿",
	"入队修复 - 修复发布时间排序引入的队列读取错误，全量下载恢复正常入队",
	"手动监测 - 雷达列表每行新增监测按钮，立即检测一次博主新视频",
}
