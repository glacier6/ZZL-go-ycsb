#!/bin/bash
# NOTE:本脚本自动切换原版与魔改版BadgerDB，进行双版本多轮、多线程压力对照压测并保存独立日志

# ================= 配置区 =================
# 定义 Badger 模块名
MODULE_NAME="github.com/dgraph-io/badger/v4"

# 定义两个版本的本地物理路径
NORMAL_BADGER_PATH="/home/hanjiang/DB-CODE/nomalBadger"
PRIZZL_BADGER_PATH="/home/hanjiang/DB-CODE/priZzlBadger"

# 定义测试数据存放目录
DATA_DIR="./zzl_badger_data"

# ================= 0. 参数解析 =================
# 获取外部传入的轮次，如果没有传参数，默认跑 1 轮
ROUNDS=${1:-1}

# 验证输入是否为正整数
if ! [[ "$ROUNDS" =~ ^[1-9][0-9]*$ ]]; then
    echo "❌ 错误: 轮次参数必须是正整数！例如: ./run_test.sh 3"
    exit 1
fi

# ================= 1. 环境准备 =================
# 自动切换到项目根目录
cd "$(dirname "$0")/.." || { echo "切换到项目根目录失败"; exit 1; }

echo "=================================================="
echo "🚀 开始一键双版本多线程对照压测流程..."
echo "🎯 计划执行轮次: ${ROUNDS} 轮"
echo "=================================================="

# ================= 核心测试逻辑函数 =================
# 封装单次测试的完整流程
# 参数: $1=版本名, $2=版本物理路径, $3=当前轮次, $4=当前线程数
run_single_test() {
    local VERSION_NAME=$1
    local BADGER_PATH=$2
    local ROUND=$3
    local THREADS=$4
    
    # 动态生成带线程数和轮次的日志文件名
    local LOG_FILE="result_${VERSION_NAME}_T${THREADS}_Round${ROUND}.log"

    echo ""
    echo "--------------------------------------------------"
    echo "🔄 版本: 【${VERSION_NAME}】 | 线程数: 【${THREADS}】 | 轮次: 【第 ${ROUND} 轮】"
    echo "--------------------------------------------------"
    
    # [步骤A]：修改 go.mod 指向目标版本
    echo "⚙️ [A] 正在修改 go.mod 依赖指向: ${BADGER_PATH}"
    go mod edit -replace ${MODULE_NAME}=${BADGER_PATH}
    go mod tidy

    # [步骤B]：清理旧数据
    echo "🧹 [B] 正在清空旧数据: ${DATA_DIR}/*"
    rm -rf ${DATA_DIR}/*
    mkdir -p ${DATA_DIR} 
    echo "✅ 旧数据清理完毕。"

    # ⚠️ 为了保证 IO 统计不受旧页缓存影响，这里需要清空 OS 缓存
    echo "🧼 正在清空操作系统页缓存 ..."
    echo "207hanjiang" | sudo -S sh -c 'sync; echo 3 > /proc/sys/vm/drop_caches'
    echo "✅ 环境净化完毕。"

    # [步骤C]：重新编译
    echo "🔨 [C] 正在重新编译 go-ycsb..."
    make build
    if [ $? -ne 0 ]; then
        echo "❌ 【${VERSION_NAME}】 编译失败，停止测试！请检查代码！"
        # 即使失败，也要把日志里的报错存下来
        exit 1
    fi
    echo "✅ 编译成功。"

    # [步骤D]：开始压测并记录日志
    echo "🏃 [D] 开始压测！日志将实时写入 -> ${LOG_FILE}"
    # NOTE:注意这里用了 > 代表是覆盖写入!
    echo "=== 【${VERSION_NAME}】 Threads: ${THREADS}, Round: ${ROUND} 测试报告 ===" > ${LOG_FILE} 
    
    # --- Load 阶段 ---
    echo "📥 执行 Load 阶段..."
    echo ">>> --- Load Phase ---" >> ${LOG_FILE}
    ./bin/go-ycsb load badger -P workloads/workloada -P zzl_badger.properties -p threadcount=${THREADS} >> ${LOG_FILE} 2>&1
    if [ $? -ne 0 ]; then
        echo "❌ Load 阶段报错，请查看 ${LOG_FILE}！"
        exit 1
    fi
    echo "✅ Load 阶段完成。"

    # 再次清空缓存
    echo "🧼 正在清空操作系统页缓存 ..."
    echo "207hanjiang" | sudo -S sh -c 'sync; echo 3 > /proc/sys/vm/drop_caches'
    echo "✅ 环境净化完毕。"

    # --- Run 阶段 ---
    echo "🔥 执行 Run 阶段..."
    echo ">>> --- Run Phase ---" >> ${LOG_FILE}
    ./bin/go-ycsb run badger -P workloads/workloada -P zzl_badger.properties -p threadcount=${THREADS} >> ${LOG_FILE} 2>&1
    
    echo "✅ Run 阶段完成。"
    echo "🏁 【${VERSION_NAME}】 T${THREADS} 第 ${ROUND} 轮测试流程结束！"
}

# ================= 主执行流程 =================
START_TIME=$(date +%s)

# 定义要测试的线程数数组
THREAD_COUNTS=(1)
# 如果需要测试特定线程，可以修改上面的数组，例如： THREAD_COUNTS=(8 16)

echo "📊 计划测试的线程数列表: ${THREAD_COUNTS[*]}"

# 外层循环：遍历设定的线程数
for TC in "${THREAD_COUNTS[@]}"; do
    echo ""
    echo "=================================================="
    echo "🔥 开始针对线程数 【${TC}】 的全面压测"
    echo "=================================================="

    # 内层循环：按照指定的轮次进行循环压测
    for (( i=1; i<=ROUNDS; i++ )); do
        echo ""
        echo "🔔 [线程数: ${TC}] 正在开始第 【${i} / ${ROUNDS}】 轮对照测试"

        # 跑魔改版 (heatLSM) Badger
        run_single_test "heatLSM_Badger_NL98M" ${PRIZZL_BADGER_PATH} $i $TC
  
        # 跑原版 Badger
        run_single_test "Normal_Badger" ${NORMAL_BADGER_PATH} $i $TC
    done
done

# 善后：把开发环境切回魔改版
echo ""
echo "🔧 测试完毕，正在将 go.mod 恢复指向开发版本..."
go mod edit -replace ${MODULE_NAME}=${PRIZZL_BADGER_PATH}
go mod tidy

END_TIME=$(date +%s)
echo "=================================================="
echo "🎉 所有 ${ROUNDS} 轮，跨越多种线程数的压测流程执行完毕！总耗时: $((END_TIME - START_TIME)) 秒。"
echo "📊 日志文件命名格式为: result_<版本>_T<线程数>_Round<轮次>.log"
echo "=================================================="