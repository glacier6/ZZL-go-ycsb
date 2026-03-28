#!/bin/bash
# NOTE:本脚本自动打包badgerDB然后开始以最新版本测试

# 1. 自动切换到项目根目录 (无论你在哪里执行这个脚本，它都会先回到 ZZL-GO-YCSB 根目录)
cd "$(dirname "$0")/.." || { echo "切换到项目根目录失败"; exit 1; }

echo "=================================================="
echo "🚀 开始一键压测流程..."
echo "=================================================="

# 2. 清空旧数据
echo "🧹 [1/4] 正在清空旧数据: ./zzl_badger_data/*"
rm -rf ./zzl_badger_data/*
# 确保文件夹存在，防止 rm 连同文件夹一起删掉后报错
mkdir -p ./zzl_badger_data 
echo "✅ 旧数据清理完毕。"

# 3. 重新编译打包项目
echo "🔨 [2/4] 正在重新编译 go-ycsb..."
make build  # 或者直接写 make，取决于你 Makefile 里的默认目标
if [ $? -ne 0 ]; then
    echo "❌ 编译失败，请检查代码后重试！"
    exit 1
fi
echo "✅ 编译成功。"

# 4. 执行 Load 阶段 
echo "📥 [3/4] 正在执行 Load 阶段 ..."
./bin/go-ycsb load badger -P workloads/workload_micro -P zzl_badger.properties -p threadcount=16
if [ $? -ne 0 ]; then
    echo "❌ Load 阶段报错，停止压测！"
    exit 1
fi
echo "✅ Load 阶段完成。"

# 5. 执行 Run 阶段
echo "🔥 [4/4] 正在执行 Run 阶段 ..."

./bin/go-ycsb run badger -P workloads/workload_micro -P zzl_badger.properties -p threadcount=16

# if [ $? -ne 0 ]; then
#     echo "❌ Run 阶段报错！"
#     exit 1
# fi

echo "=================================================="
echo "🎉 所有压测流程执行完毕！请查看上方输出的统计数据。"
echo "=================================================="