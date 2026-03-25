package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	// ⚠️ 注意：请根据你 go.mod 里的实际 badger 版本调整这里的包名
	// 通常 go-ycsb 用的是 v3 或者更早的版本
	badger "github.com/dgraph-io/badger/v4"
)

func main() {
	// 1. 定义命令行参数，默认输出 10 条，用 -limit 可以自定义
	limit := flag.Int("limit", 100, "控制输出的数据条数")
	flag.Parse()

	fmt.Printf("📂 正在打开 BadgerDB，准备读取前 %d 条数据...\n", *limit)

	// 2. 打开数据库 (关闭默认的冗长日志，让终端输出更干净)
	opt := badger.DefaultOptions("./zzl_badger_data").WithLogger(nil)
	db, err := badger.Open(opt)
	if err != nil {
		log.Fatalf("❌ 打开数据库失败: %v\n请确保没有任何 go-ycsb 进程正在运行（解除锁占用）", err)
	}
	defer db.Close()

	// 3. 开启一个只读事务 (View)
	err = db.View(func(txn *badger.Txn) error {
		// 创建迭代器
		itOpt := badger.DefaultIteratorOptions
		itOpt.PrefetchValues = true
		it := txn.NewIterator(itOpt)
		defer it.Close()

		count := 0
		fmt.Println(strings.Repeat("-", 60))

		// 遍历数据库
		for it.Rewind(); it.Valid(); it.Next() {
			if count >= *limit {
				break
			}

			item := it.Item()
			key := item.Key()

			// 读取 Value
			err := item.Value(func(val []byte) error {
				// 💡 架构师提示：YCSB 存的 Value 通常是 1KB 左右的二进制数据（包含 field0~field9）
				// 全打出来会刷屏且乱码。所以我们打印 Key，并且只打印 Value 的长度和前 30 个字节看看样子
				valStr := string(val)
				if len(valStr) > 30 {
					valStr = valStr[:30] + "..."
				}

				fmt.Printf("[%d] Key: %s \n    ValueSize: %d bytes, ValuePreview: %s\n",
					count+1, string(key), item.ValueSize(), valStr)
				return nil
			})

			if err != nil {
				return err
			}
			count++
		}

		fmt.Println(strings.Repeat("-", 60))
		fmt.Printf("✅ 成功输出了 %d 条记录。\n", count)
		return nil
	})

	if err != nil {
		log.Fatalf("❌ 遍历数据时发生错误: %v", err)
	}
}

// 为了 strings.Repeat，这里需要引入 strings 包
