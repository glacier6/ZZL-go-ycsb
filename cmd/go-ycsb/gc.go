package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/pingcap/go-ycsb/pkg/client"
	"github.com/spf13/cobra"
)

// GCableDB 是支持 Vlog 垃圾回收操作的 DB 必须实现的接口。
// go-ycsb 的 badgerDB wrapper (db/badger/db.go) 已实现此接口。
type GCableDB interface {
	// RunValueLogGC 对垃圾率超过 discardRatio 的 Vlog 文件执行一次 GC。
	// Badger 内部自动选出垃圾最多的文件（跳过当前正在写入的活跃文件）。
	// 如果没有符合条件的文件，返回 ErrNoRewrite。
	RunValueLogGC(discardRatio float64) error

	// VlogSize 返回 Badger.Size() 的 (LSM大小, Vlog大小)，单位字节。
	VlogSize() (lsm int64, vlog int64)

	// VlogStats 返回 Vlog 文件的详细大盘统计（每个文件的尺寸和垃圾率）。
	// heatLSM 版 Badger 通过 VlogStatsToString() 提供；
	// 原生 Badger 无此公开 API，返回空字符串，此时回退到手动扫描 Vlog 目录。
	VlogStats() string
}

// vlogFile 表示一个 .vlog 文件及其实际磁盘占用。
// size 是实际占用的磁盘块大小 (st_blocks*512)，不是表观大小 (st_size)。
// Badger 创建 Vlog 文件时会 Truncate 到 ValueLogFileSize (如 1GB)，
// 但这只是预分配的空洞（sparse file），info.Size() 拿到的表观大小会严重失真。
type vlogFile struct {
	name string // 文件名，如 "000001.vlog"
	size int64  // 实际磁盘块占用 (st_blocks*512)，单位字节
}

// scanVlogDir 扫描指定目录下所有 .vlog 文件，按文件名排序返回。
// 文件大小使用 st_blocks*512 (实际磁盘占用)，而非 st_size (表观/预分配大小)。
// 用于在原生 Badger（无 VlogStatsToString 公开 API）时的手动统计。
func scanVlogDir(dir string) ([]vlogFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []vlogFile
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".vlog") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			// 使用实际磁盘块占用 (st_blocks*512) 而非表观大小 (st_size)。
			// Badger 创建 Vlog 文件时 Truncate 预分配空间 → st_size 虚高。
			// st_blocks*512 才反映真实落盘数据量。
			actualSize := info.Size() // fallback: 非 Linux 系统退回到表观大小
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				actualSize = stat.Blocks * 512
			}
			files = append(files, vlogFile{name: e.Name(), size: actualSize})
		}
	}
	// 按文件名排序，方便观察 Vlog 文件的生成时间线
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

// formatVlogDirStats 将 Vlog 目录扫描结果格式化为可读的表格字符串。
func formatVlogDirStats(dir string) string {
	files, err := scanVlogDir(dir)
	if err != nil {
		return fmt.Sprintf("⚠️  无法扫描目录 %s: %v\n", dir, err)
	}
	if len(files) == 0 {
		return fmt.Sprintf("📂 %s: 无 .vlog 文件\n", dir)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📂 Vlog 目录: %s (%d 个文件)\n", dir, len(files)))
	sb.WriteString("──────────────────────────────────────────────\n")
	var totalSize int64
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("  %-30s %10.2f MB\n", f.name, float64(f.size)/(1<<20)))
		totalSize += f.size
	}
	sb.WriteString("──────────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  总计 (实际磁盘占用): %.2f MB\n", float64(totalSize)/(1<<20)))
	return sb.String()
}

// totalVlogSize 返回 Vlog 目录下所有 .vlog 文件的【实际磁盘占用】总和，失败返回 -1。
// 注意：使用的是 st_blocks*512 (实际块)，而非 st_size (预分配表观大小)。
func totalVlogSize(dir string) int64 {
	files, err := scanVlogDir(dir)
	if err != nil {
		return -1
	}
	var total int64
	for _, f := range files {
		total += f.size
	}
	return total
}

// readWriteBytes 从 /proc/self/io 读取当前进程的累计物理写入字节数。
// 这是操作系统内核记录的"生死簿"——包含所有写入磁盘的字节，
// 包括同步写、异步写、page cache 回写等。
// 仅在 Linux 环境下有效，其他系统返回 -1。
func readWriteBytes() int64 {
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return -1
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "write_bytes:") {
			var v int64
			fmt.Sscanf(line, "write_bytes: %d", &v)
			return v
		}
	}
	return -1
}

// newGCCommand 创建 gc 子命令。
// 用法: ./bin/go-ycsb gc badger -P zzl_badger.properties -p gc.count=3 -p gc.discard_ratio=0.5
func newGCCommand() *cobra.Command {
	m := &cobra.Command{
		Use:   "gc db",
		Short: "Run value log garbage collection on BadgerDB and report statistics",
		Long: `Run value log garbage collection on BadgerDB.

Performs N rounds of GC (default 3), targeting the vlog files with the
highest garbage ratio each round. Reports per-round duration, additional
OS physical disk writes, and space savings.

Currently only supports the "badger" database backend.`,
		Args: cobra.MinimumNArgs(1),
		Run:  runGCCommandFunc,
	}
	m.Flags().StringSliceVarP(&propertyFiles, "property_file", "P", nil, "Specify a property file")
	m.Flags().StringSliceVarP(&propertyValues, "prop", "p", nil, "Specify a property value with name=value")
	return m
}

// runGCCommandFunc 是 gc 子命令的实际执行逻辑。
//
// 工作流程：
//  1. 打开 BadgerDB，获取 GC 接口
//  2. 拍摄 GC 前快照（Vlog 文件列表 + 磁盘占用 + Badger.Size()）
//  3. 执行 N 轮 GC，每轮记录：耗时、额外的 OS 物理写入增量、空间回收量
//  4. 拍摄 GC 后快照
//  5. 输出汇总报告（空间变化、GC 造成的额外磁盘总写入、逐轮明细）
//
// 核心指标说明：
//   - 额外磁盘物理写入 = 本轮 GC 后 /proc/self/io 的 write_bytes - 本轮 GC 前的值
//     即"跑这一次 GC，又造成了多少额外的磁盘写入"。
//     这些写入来自 GC 重写 Vlog 文件中未过期的有效 KV 数据。
//   - 空间放大变化 = GC 前 (LSM + Vlog) - GC 后 (LSM + Vlog)
//     正值表示成功回收了空间，负值表示 GC 过程中产生的临时数据尚未合并。
func runGCCommandFunc(cmd *cobra.Command, args []string) {
	dbName := args[0]
	if dbName != "badger" {
		fmt.Printf("❌ GC 命令目前仅支持 badger 数据库, 收到: %s\n", dbName)
		return
	}

	// 复用 main.go 的初始化逻辑：解析属性 → 创建 Workload → 创建 DB
	initialGlobal(dbName, nil)

	// ---- 解开包装链：globalDB (client.DbWrapper) → inner DB (*badgerDB) ----
	// globalDB 的类型是 ycsb.DB 接口，实际值为 client.DbWrapper{DB: *badgerDB}
	wrapper, ok := globalDB.(client.DbWrapper)
	if !ok {
		fmt.Println("❌ 无法访问底层数据库 (类型断言失败)")
		return
	}

	// *badgerDB 实现了 GCableDB 接口（在 db/badger/db.go 中代理到 *badger.DB）
	gcDB, ok := wrapper.DB.(GCableDB)
	if !ok {
		fmt.Println("❌ 此数据库不支持 GC 操作")
		return
	}

	// ---- 从属性中读取 GC 参数 ----
	// gc.count: GC 执行轮数，默认 3。每轮自动选出垃圾率最高的 Vlog 文件。
	gcCount := int(globalProps.GetInt64("gc.count", 3))
	if gcCount < 1 {
		gcCount = 1
	}
	// gc.discard_ratio: 丢弃阈值，默认 0.5。
	// 只有当文件的垃圾占比 ≥ 此阈值时，Badger 才会对该文件执行 GC 重写。
	// 设 0.5 则理论 Vlog 写放大趋近于 2（1 + 0.5 + 0.25 + ... = 2）。
	discardRatio := globalProps.GetFloat64("gc.discard_ratio", 0.5)
	if discardRatio <= 0.0 || discardRatio >= 1.0 {
		discardRatio = 0.5
	}

	// 确定 Vlog 文件所在目录（优先 valuedir，未设则用 dir）
	vlogDir := globalProps.GetString("badger.valuedir", "")
	if vlogDir == "" {
		vlogDir = globalProps.GetString("badger.dir", "/tmp/badger")
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║          🧹 BadgerDB Vlog GC 分析工具           ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf("║  GC 轮次: %d         丢弃阈值: %.0f%%                ║\n", gcCount, discardRatio*100)
	fmt.Printf("║  Vlog 目录: %s  ║\n", vlogDir)
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()

	// ==================== GC 前快照 ====================
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📸 【GC 前】 快照")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 优先使用 Badger 自带的详细统计（heatLSM 提供 per-file 垃圾率）
	if stats := gcDB.VlogStats(); stats != "" {
		fmt.Print(stats)
	} else {
		// 原生 Badger 回退：手动扫描 Vlog 目录，只能看到文件大小，看不到垃圾率
		fmt.Print(formatVlogDirStats(vlogDir))
	}

	// 记录 GC 前的 Badger 内部统计值，用于后续计算空间放大变化
	preLsm, preVlog := gcDB.VlogSize()
	preVlogDirSize := totalVlogSize(vlogDir)
	fmt.Printf("📊 Badger.Size() 报告: LSM=%d MB | Vlog=%d MB\n",
		preLsm/(1<<20), preVlog/(1<<20))
	fmt.Println()

	// ==================== 执行多轮 GC ====================
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("🔥 开始执行 %d 轮 GC (discardRatio=%.2f)\n", gcCount, discardRatio)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// gcRoundResult 记录单轮 GC 的全部指标
	type gcRoundResult struct {
		round            int           // 轮次编号 (1-based)
		duration         time.Duration // 本轮 GC 耗时
		physWriteBefore  int64         // GC 前 OS 物理写入累计值
		physWriteAfter   int64         // GC 后 OS 物理写入累计值
		physWriteDelta   int64         // 本轮 GC 产生的物理写入增量
		spaceReclaimedMB float64       // 本轮回收的磁盘空间 (Vlog 目录大小变化)
		lsmBefore        int64         // GC 前 LSM 大小
		vlogBefore       int64         // GC 前 Vlog 大小
		lsmAfter         int64         // GC 后 LSM 大小
		vlogAfter        int64         // GC 后 Vlog 大小
		err              error         // GC 返回的错误 (nil = 成功, ErrNoRewrite = 无可回收文件)
	}

	var results []gcRoundResult
	var totalDuration time.Duration        // 所有轮次总耗时
	var totalPhysWrite int64               // 所有轮次总 OS 物理写入
	var cumulativeSpaceReclaimedMB float64 // 累计空间回收量

	for i := 1; i <= gcCount; i++ {
		fmt.Printf("\n--- 🔄 GC 第 %d/%d 轮 ---\n", i, gcCount)

		// ---- 本轮开始前采样 ----
		physBefore := readWriteBytes()           // OS 物理写入量 (用于计算本轮增量)
		lsmBefore, vlogBefore := gcDB.VlogSize() // Badger 内部统计
		vlogDirBefore := totalVlogSize(vlogDir)  // Vlog 目录磁盘占用
		start := time.Now()

		// ---- 执行 GC：Badger 自动选出垃圾最多的 Vlog 文件，重写其有效数据 ----
		// RunValueLogGC 内部流程：
		//   1. pickLog(discardRatio) → 从 discardStats 中找 fid<maxFid 且垃圾率≥阈值的文件
		//   2. doRunGC → rewrite(lf) → 把该文件中未过期的 KV 重新写入 LSM + 新 Vlog
		//   3. 删除旧 Vlog 文件，更新 discardStats
		// NOTE: maxFid（当前活跃写入文件）不会被选中，保证了不会 GC 正在写入的文件。
		err := gcDB.RunValueLogGC(discardRatio)

		// ---- 本轮结束后采样 ----
		elapsed := time.Since(start)
		physAfter := readWriteBytes()
		lsmAfter, vlogAfter := gcDB.VlogSize()
		vlogDirAfter := totalVlogSize(vlogDir)

		result := gcRoundResult{
			round:           i,
			duration:        elapsed,
			physWriteBefore: physBefore,
			physWriteAfter:  physAfter,
			lsmBefore:       lsmBefore,
			vlogBefore:      vlogBefore,
			lsmAfter:        lsmAfter,
			vlogAfter:       vlogAfter,
			err:             err,
		}

		// 计算本轮 OS 物理写入增量
		if physBefore >= 0 && physAfter >= 0 {
			result.physWriteDelta = physAfter - physBefore
			if result.physWriteDelta < 0 {
				result.physWriteDelta = 0 // 防御性：不可能为负
			}
		}

		// 计算本轮空间回收量 = Vlog 目录磁盘占用的减少量
		// (GC 删除旧 Vlog 文件 → 目录变小；GC 重写产生新 Vlog 文件 → 目录变大)
		if vlogDirBefore >= 0 && vlogDirAfter >= 0 {
			result.spaceReclaimedMB = float64(vlogDirBefore-vlogDirAfter) / (1 << 20)
		}

		// 累加到总计
		totalDuration += elapsed
		totalPhysWrite += result.physWriteDelta
		cumulativeSpaceReclaimedMB += result.spaceReclaimedMB

		// ---- 打印本轮结果 ----
		fmt.Printf("   ⏱️  耗时: %v\n", elapsed.Round(time.Microsecond))
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "ErrNoRewrite") || strings.Contains(errStr, "No file") {
				fmt.Println("   ℹ️  状态: 无可回收文件 (ErrNoRewrite) — 提前结束")
			} else {
				fmt.Printf("   ⚠️  状态: 错误 — %v\n", err)
			}
		} else {
			fmt.Println("   ✅ 状态: 成功回收")
		}

		// 本轮 GC 造成的额外磁盘物理写入
		if result.physWriteDelta > 0 {
			physMB := float64(result.physWriteDelta) / (1 << 20)
			fmt.Printf("   💿 本轮 GC 额外造成的磁盘物理写入: %.2f MB\n", physMB)
		}

		if result.spaceReclaimedMB > 0 {
			fmt.Printf("   🗑️  空间回收: %.2f MB\n", result.spaceReclaimedMB)
		}

		// 展示 Badger 内部统计的 LSM/Vlog 大小变化
		lsmDelta := (lsmAfter - lsmBefore) / (1 << 20)
		vlogDelta := (vlogAfter - vlogBefore) / (1 << 20)
		if lsmDelta != 0 || vlogDelta != 0 {
			fmt.Printf("   📏 Badger.Size() 变化: LSM %+d MB | Vlog %+d MB\n", lsmDelta, vlogDelta)
		}

		results = append(results, result)

		// 遇到 ErrNoRewrite 说明已经没有可回收的文件了，提前退出循环
		if err != nil && (strings.Contains(err.Error(), "ErrNoRewrite") || strings.Contains(err.Error(), "No file")) {
			fmt.Printf("   🛑 没有更多可回收文件，提前结束。\n")
			break
		}
	}

	// ==================== GC 后快照 ====================
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📸 【GC 后】 快照")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if stats := gcDB.VlogStats(); stats != "" {
		fmt.Print(stats)
	} else {
		fmt.Print(formatVlogDirStats(vlogDir))
	}

	postLsm, postVlog := gcDB.VlogSize()
	postVlogDirSize := totalVlogSize(vlogDir)
	fmt.Printf("📊 Badger.Size() 报告: LSM=%d MB | Vlog=%d MB\n",
		postLsm/(1<<20), postVlog/(1<<20))

	// ==================== 汇总报告 ====================
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║              📊 GC 汇总报告                      ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")

	roundsExecuted := len(results)
	fmt.Printf("║  实际执行轮次: %d / %d                             ║\n", roundsExecuted, gcCount)
	fmt.Printf("║  总耗时: %v                                ║\n", totalDuration.Round(time.Millisecond))

	// ---- 空间变化 ----
	// Badger.Size() = 逻辑数据量（LSM树内数据 + Vlog内有效数据）。
	// GC 只是把旧 Vlog 中未过期的有效数据搬到 LSM / 新 Vlog，并不会删除有效数据，
	// 所以 Badger.Size() 在 GC 前后几乎不变是正常的。
	// 真正减少的是 Vlog 目录的磁盘占用（下面一行），因为我们用 st_blocks 测量实际块。
	totalSizeBefore := preLsm + preVlog
	totalSizeAfter := postLsm + postVlog
	totalSizeDelta := totalSizeBefore - totalSizeAfter
	totalSizeDeltaMB := float64(totalSizeDelta) / (1 << 20)

	fmt.Println("╠──────────────────────────────────────────────────╣")
	fmt.Println("║  📏 空间变化 (Badger 逻辑视角)                    ║")
	fmt.Printf("║    GC 前总逻辑数据 (LSM+Vlog): %d MB              ║\n", totalSizeBefore/(1<<20))
	fmt.Printf("║    GC 后总逻辑数据 (LSM+Vlog): %d MB              ║\n", totalSizeAfter/(1<<20))
	if totalSizeDelta >= 0 {
		fmt.Printf("║    逻辑数据变化: -%.2f MB (数据被搬到 LSM 紧凑存储)║\n", totalSizeDeltaMB)
	} else {
		fmt.Printf("║    逻辑数据变化: +%.2f MB (GC 重写产生了临时数据)  ║\n", -totalSizeDeltaMB)
	}

	// Vlog 目录【实际磁盘块占用】的变化 —— 这才是 GC 真正回收的空间
	if preVlogDirSize >= 0 && postVlogDirSize >= 0 {
		vlogDirDelta := float64(preVlogDirSize-postVlogDirSize) / (1 << 20)
		if vlogDirDelta >= 0 {
			fmt.Printf("║    ✅ Vlog 目录实际磁盘占用减少: %.2f MB           ║\n", vlogDirDelta)
		} else {
			fmt.Printf("║    ⚠️  Vlog 目录实际磁盘占用增加: %.2f MB          ║\n", -vlogDirDelta)
		}
	}

	// ---- GC 造成的额外磁盘总写入 ----
	fmt.Println("╠──────────────────────────────────────────────────╣")
	fmt.Println("║  💿 GC 造成的额外磁盘写入                         ║")
	if totalPhysWrite > 0 {
		totalPhysMB := float64(totalPhysWrite) / (1 << 20)
		fmt.Printf("║    GC 总物理写入: %.2f MB                         ║\n", totalPhysMB)
		if cumulativeSpaceReclaimedMB > 0 {
			fmt.Printf("║    (总共回收了 %.2f MB 磁盘空间)                   ║\n", cumulativeSpaceReclaimedMB)
		}
	} else {
		fmt.Println("║    (无法获取进程 I/O 统计)                        ║")
	}

	// ---- 逐轮明细 ----
	fmt.Println("╠──────────────────────────────────────────────────╣")
	fmt.Println("║  🔍 逐轮明细                                     ║")
	for _, r := range results {
		status := "✅"
		if r.err != nil {
			if strings.Contains(r.err.Error(), "ErrNoRewrite") {
				status = "⏭️ " // 无可回收文件，跳过
			} else {
				status = "⚠️ " // 异常错误
			}
		}
		physDeltaMB := float64(r.physWriteDelta) / (1 << 20)
		fmt.Printf("║  R%d %s 耗时:%10v | 物理写:%7.2f MB | 回收:%7.2f MB ║\n",
			r.round, status, r.duration.Round(time.Microsecond),
			physDeltaMB, r.spaceReclaimedMB)
	}

	fmt.Println("╚══════════════════════════════════════════════════╝")

	// 输出机器可解析行，方便 Bash 脚本通过 grep 提取关键指标
	// 格式: >>> [GC_SUMMARY] rounds=3 total_time_ms=1234 phys_write_mb=56.78 space_reclaimed_mb=90.12
	fmt.Printf(">>> [GC_SUMMARY] rounds=%d total_time_ms=%d phys_write_mb=%.2f space_reclaimed_mb=%.2f\n",
		roundsExecuted, totalDuration.Milliseconds(),
		float64(totalPhysWrite)/(1<<20), cumulativeSpaceReclaimedMB)
	fmt.Println("╚═════════════════════✅✅✅GC已经结束✅✅✅═════════════════════════════╝")
	fmt.Println("╚═════════════════════✅✅✅GC已经结束✅✅✅═════════════════════════════╝")
	fmt.Println("╚═════════════════════✅✅✅GC已经结束✅✅✅═════════════════════════════╝")
}
