package main

import (
	"runtime/debug"

	"wx_channel/cmd"
)

func main() {
	// GC 调优：适度收紧以降低常驻内存。
	// 之前为 200（堆阈值放大一倍），导致常驻内存偏高；
	// 现在 100（默认值）+ 软内存上限 256MiB，在下载吞吐与内存占用间取平衡。
	debug.SetGCPercent(100)
	debug.SetMemoryLimit(256 << 20)

	cmd.Execute()
}
