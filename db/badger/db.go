// Copyright 2018 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package badger

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/dgraph-io/badger/v4"
	"github.com/magiconair/properties"
	"github.com/pingcap/go-ycsb/pkg/prop"
	"github.com/pingcap/go-ycsb/pkg/util"
	"github.com/pingcap/go-ycsb/pkg/ycsb"
)

// properties
const (
	badgerDir                     = "badger.dir"
	badgerValueDir                = "badger.valuedir"
	badgerSyncWrites              = "badger.sync_writes"
	badgerNumVersionsToKeep       = "badger.num_versions_to_keep"
	badgerMaxTableSize            = "badger.max_table_size"
	badgerLevelSizeMultiplier     = "badger.level_size_multiplier"
	badgerMaxLevels               = "badger.max_levels"
	badgerValueThreshold          = "badger.value_threshold"
	badgerNumMemtables            = "badger.num_memtables"
	badgerNumLevelZeroTables      = "badger.num_level0_tables"
	badgerNumLevelZeroTablesStall = "badger.num_level0_tables_stall"
	badgerLevelOneSize            = "badger.level_one_size"
	badgerValueLogFileSize        = "badger.value_log_file_size"
	badgerValueLogMaxEntries      = "badger.value_log_max_entries"
	badgerNumCompactors           = "badger.num_compactors"
	badgerDoNotCompact            = "badger.do_not_compact"
	badgerTableLoadingMode        = "badger.table_loading_mode"
	badgerValueLogLoadingMode     = "badger.value_log_loading_mode"
	// TODO: add more configurations
)

type badgerCreator struct {
}

type badgerDB struct {
	p *properties.Properties

	db *badger.DB

	r       *util.RowCodec
	bufPool *util.BufPool
	//zzlHACK: 💥 新增：本地累加器（无锁）
	localBytesCount int64
}

var GlobalLogicalWriteBytes atomic.Int64 //zzlHACK: 💥 新增：全局累加器

type contextKey string

const stateKey = contextKey("badgerDB")

type badgerState struct {
}

func (c badgerCreator) Create(p *properties.Properties) (ycsb.DB, error) {
	opts := getOptions(p)

	if p.GetBool(prop.DropData, prop.DropDataDefault) {
		os.RemoveAll(opts.Dir)
		os.RemoveAll(opts.ValueDir)
	}

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &badgerDB{
		p:       p,
		db:      db,
		r:       util.NewRowCodec(p),
		bufPool: util.NewBufPool(),
	}, nil
}

func getOptions(p *properties.Properties) badger.Options {
	dir := p.GetString(badgerDir, "/tmp/badger")
	opts := badger.DefaultOptions(dir)

	opts.ValueDir = p.GetString(badgerValueDir, opts.Dir)

	opts.SyncWrites = p.GetBool(badgerSyncWrites, false)
	opts.NumVersionsToKeep = p.GetInt(badgerNumVersionsToKeep, 1)

	// opts.MaxTableSize = p.GetInt64(badgerMaxTableSize, 64<<20)
	opts.MemTableSize = p.GetInt64(badgerMaxTableSize, 64<<20)

	opts.LevelSizeMultiplier = p.GetInt(badgerLevelSizeMultiplier, 10)
	opts.MaxLevels = p.GetInt(badgerMaxLevels, 7)

	// opts.ValueThreshold = p.GetInt(badgerValueThreshold, 32)

	opts.NumMemtables = p.GetInt(badgerNumMemtables, 5)
	opts.NumLevelZeroTables = p.GetInt(badgerNumLevelZeroTables, 5)
	opts.NumLevelZeroTablesStall = p.GetInt(badgerNumLevelZeroTablesStall, 15) // V1是10,但是V4默认就是15了

	// opts.LevelOneSize = p.GetInt64(badgerLevelOneSize, 256<<20)
	opts.BaseLevelSize = p.GetInt64(badgerLevelOneSize, 10<<20) // 由于原来的设定L0大小改为了设定LBase（就是倒立漏斗拐角层的大小，也是倒立漏斗上面那部分）

	opts.ValueLogFileSize = p.GetInt64(badgerValueLogFileSize, 1<<30-1)
	opts.ValueLogMaxEntries = uint32(p.GetUint64(badgerValueLogMaxEntries, 1000000))
	opts.NumCompactors = p.GetInt(badgerNumCompactors, 4)
	if p.GetBool(badgerDoNotCompact, false) {
		opts.NumCompactors = 0
	}
	// zzlHACK:加速压缩,暂时测试用
	// 首个目标上限比预设的层基础大小（BaseLevelSize）小或等的作为基线层
	// 下面这三个是默认的
	// MemTableSize:        64 << 20, //内存表的总尺寸大小 64乘2的20次方（向左移位20次），64MB
	// BaseTableSize:       2 << 20,  //SST大小，2MB
	// BaseLevelSize:       10 << 20,
	// ValueThreshold:      1 << 20
	// opts.MemTableSize = 4 << 20
	// opts.BaseLevelSize = 8 << 20 // 这个调整的是到base的条件,即倒立漏斗的拐弯处大小
	// opts.LevelSizeMultiplier = 2 // 这个调整的是倒立漏斗的倾斜程度,越大则越倾斜,底层容纳的数据量也就越大!层级倍率调整为5倍的话,10G的数据即可让BASE到1层(1层为3mb,2层为16mb).为3倍的话,1G的数据就可以
	opts.ValueThreshold = 1 << 10 // NOTE:2026062200 修改分裂阈值
	// zzlHACK:END

	// if b := p.GetString(badgerTableLoadingMode, "LoadToRAM"); len(b) > 0 {
	// 	if b == "FileIO" {
	// 		opts.TableLoadingMode = options.FileIO
	// 	} else if b == "LoadToRAM" {
	// 		opts.TableLoadingMode = options.LoadToRAM
	// 	} else if b == "MemoryMap" {
	// 		opts.TableLoadingMode = options.MemoryMap
	// 	}
	// }
	// if b := p.GetString(badgerValueLogLoadingMode, "MemoryMap"); len(b) > 0 {
	// 	if b == "FileIO" {
	// 		opts.ValueLogLoadingMode = options.FileIO
	// 	} else if b == "LoadToRAM" {
	// 		opts.ValueLogLoadingMode = options.LoadToRAM
	// 	} else if b == "MemoryMap" {
	// 		opts.ValueLogLoadingMode = options.MemoryMap
	// 	}
	// }

	return opts
}

func (db *badgerDB) Close() error {
	// zzlHACK:清理尾部以及输出逻辑写入量
	if db.localBytesCount > 0 {
		GlobalLogicalWriteBytes.Add(db.localBytesCount)
		db.localBytesCount = 0
	}
	logicalMB := float64(GlobalLogicalWriteBytes.Load()) / (1 << 20)

	// =====================================================================
	// 💥 第一步：先调用底层 Badger 的 Close！
	// 这会阻塞等待所有后台 Compaction、Memtable Flush 彻底、绝对地写入物理磁盘！
	// =====================================================================
	closeErr := db.db.Close()

	// =====================================================================
	// 💥 第二步：此时引擎已经完全静止，底层 I/O 彻底结束。
	// 我们亲自去读操作系统内核为我们记录的“生死簿”！
	// =====================================================================
	var physicalBytes int64
	ioData, err := os.ReadFile("/proc/self/io") // 注意这里只会看YCSB自身的IO量
	if err == nil {
		lines := strings.Split(string(ioData), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "write_bytes:") {
				fmt.Sscanf(line, "write_bytes: %d", &physicalBytes)
				break
			}
		}
	}

	physicalMB := float64(physicalBytes) / (1 << 20)
	var finalWA float64
	if logicalMB > 0 {
		finalWA = physicalMB / logicalMB
	}

	// 华丽地打印出终极学术对账单
	fmt.Printf("\n=======================================================\n")
	fmt.Printf("🏆 [YCSB 终极结算] 压测及引擎落盘已全部完成\n")
	fmt.Printf("=======================================================\n")
	fmt.Printf("🎯 [分母] 纯应用层逻辑写入: %.2f MB\n", logicalMB)
	if physicalBytes > 0 {
		fmt.Printf("💿 [分子] OS内核级物理写入: %.2f MB  (%d Bytes)\n", physicalMB, physicalBytes)
		fmt.Printf("🔥 [核弹级数据] 最终系统级写放大 (System WA): %.2f x\n", finalWA)
		// 留下一个特殊标记，方便外面的 Bash 脚本直接 Grep 提取
		fmt.Printf(">>> [PHYSICAL_IO_RESULT] %.2f\n", physicalMB)
	} else {
		fmt.Printf("⚠️ 无法读取 /proc/self/io，请确保在 Linux 环境下运行！\n")
	}
	fmt.Printf("=======================================================\n\n")
	// zzlHACK:END
	return closeErr
}

func (db *badgerDB) InitThread(ctx context.Context, _ int, _ int) context.Context {
	return ctx
}

func (db *badgerDB) CleanupThread(_ context.Context) {
}

func (db *badgerDB) getRowKey(table string, key string) []byte {
	return util.Slice(fmt.Sprintf("%s:%s", table, key))
}

func (db *badgerDB) Read(ctx context.Context, table string, key string, fields []string) (map[string][]byte, error) {
	var m map[string][]byte
	err := db.db.View(func(txn *badger.Txn) error {
		rowKey := db.getRowKey(table, key)
		item, err := txn.Get(rowKey)
		if err != nil {
			return err
		}
		row, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		m, err = db.r.Decode(row, fields)
		return err
	})

	return m, err
}

func (db *badgerDB) Scan(ctx context.Context, table string, startKey string, count int, fields []string) ([]map[string][]byte, error) {
	res := make([]map[string][]byte, count)
	err := db.db.View(func(txn *badger.Txn) error {
		rowStartKey := db.getRowKey(table, startKey)
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		i := 0
		for it.Seek(rowStartKey); it.Valid() && i < count; it.Next() {
			item := it.Item()
			value, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			m, err := db.r.Decode(value, fields)
			if err != nil {
				return err
			}

			res[i] = m
			i++
		}

		return nil
	})

	return res, err
}

func (db *badgerDB) Update(ctx context.Context, table string, key string, values map[string][]byte) error {
	err := db.db.Update(func(txn *badger.Txn) error {
		rowKey := db.getRowKey(table, key)
		item, err := txn.Get(rowKey)
		if err != nil {
			return err
		}

		value, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		data, err := db.r.Decode(value, nil)
		if err != nil {
			return err
		}

		for field, value := range values {
			data[field] = value
		}

		buf := db.bufPool.Get()
		defer func() {
			db.bufPool.Put(buf)
		}()

		buf, err = db.r.Encode(buf, data)
		if err != nil {
			return err
		}
		// 🌟 新增深拷贝：切断与 YCSB 内存池的联系
		safeBuf := make([]byte, len(buf))
		copy(safeBuf, buf)

		// zzlHACK:统计逻辑写入量
		payloadSize := int64(len(rowKey) + len(safeBuf))
		db.localBytesCount += payloadSize
		if db.localBytesCount >= 1048576 {
			GlobalLogicalWriteBytes.Add(db.localBytesCount)
			db.localBytesCount = 0 // 清零重新攒
		}
		// zzlHACK:END
		// fmt.Printf("👉 [BadgerDB Insert] 准备更新 Key: %s, 真实 Value 长度: %d bytes\n", rowKey, len(safeBuf))
		// 传入独立内存 safeBuf，避免并发覆写
		return txn.Set(rowKey, safeBuf)
	})
	return err
}

func (db *badgerDB) Insert(ctx context.Context, table string, key string, values map[string][]byte) error {
	err := db.db.Update(func(txn *badger.Txn) error {
		// fmt.Printf("insert %s\n", key)
		rowKey := db.getRowKey(table, key)

		buf := db.bufPool.Get()
		defer func() {
			db.bufPool.Put(buf)
		}()

		buf, err := db.r.Encode(buf, values)
		if err != nil {
			return err
		}
		// 🌟 新增深拷贝：切断与 YCSB 内存池的联系
		safeBuf := make([]byte, len(buf))
		copy(safeBuf, buf)

		// fmt.Printf("👉 [BadgerDB Insert] 准备写入 Key: %s, 真实 Value 长度: %d bytes\n", rowKey, len(safeBuf))
		// zzlHACK:统计逻辑写入量
		payloadSize := int64(len(rowKey) + len(safeBuf))
		db.localBytesCount += payloadSize
		if db.localBytesCount >= 1048576 {
			GlobalLogicalWriteBytes.Add(db.localBytesCount)
			db.localBytesCount = 0 // 清零重新攒
		}
		// zzlHACK:END
		// 传入独立内存 safeBuf，让 Badger 慢慢去异步落盘
		return txn.Set(rowKey, safeBuf)
	})

	return err
}

func (db *badgerDB) Delete(ctx context.Context, table string, key string) error {
	err := db.db.Update(func(txn *badger.Txn) error {
		// zzlHACK:💥 墓碑写入也算逻辑写
		rowKey := db.getRowKey(table, key)
		payloadSize := int64(len(rowKey))
		db.localBytesCount += payloadSize
		if db.localBytesCount >= 1048576 {
			GlobalLogicalWriteBytes.Add(db.localBytesCount)
			db.localBytesCount = 0
		}
		// zzlHACK:END

		return txn.Delete(db.getRowKey(table, key))
	})

	return err
}

// zzlHACK: 暴露 GC 接口给命令行 gc 子命令使用
func (db *badgerDB) RunValueLogGC(discardRatio float64) error {
	return db.db.RunValueLogGC(discardRatio)
}

// zzlHACK: 暴露 Size 接口
func (db *badgerDB) VlogSize() (lsm int64, vlog int64) {
	return db.db.Size()
}

// zzlHACK: 尝试获取 Vlog 大盘统计 (如果底层 badger 支持)
func (db *badgerDB) VlogStats() string {
	if v, ok := interface{}(db.db).(interface{ VlogStatsToString() string }); ok {
		return v.VlogStatsToString()
	}
	return ""
}

func init() {
	ycsb.RegisterDBCreator("badger", badgerCreator{})
}
