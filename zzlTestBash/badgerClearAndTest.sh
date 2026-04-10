#!/bin/bash
# NOTE:本脚本自动切换原版与魔改版BadgerDB，进行双版本对照压测并保存独立日志

# ================= 配置区 =================
# 定义 Badger 模块名 (如果你代码里引用的名字不同，请修改这里)
MODULE_NAME="github.com/dgraph-io/badger/v4"

# 定义两个版本的本地物理路径 (请确保路径绝对正确)
NORMAL_BADGER_PATH="/home/hanjiang/DB-CODE/nomalBadger"
PRIZZL_BADGER_PATH="/home/hanjiang/DB-CODE/priZzlBadger"

# 定义测试数据存放目录
DATA_DIR="./zzl_badger_data"

# ================= 1. 环境准备 =================
# 自动切换到项目根目录
cd "$(dirname "$0")/.." || { echo "切换到项目根目录失败"; exit 1; }

echo "=================================================="
echo "🚀 开始一键双版本对照压测流程..."
echo "=================================================="

# ================= 核心测试逻辑函数 =================
# 封装单次测试的完整流程
run_single_test() {
    local VERSION_NAME=$1
    local BADGER_PATH=$2
    local LOG_FILE="result_${VERSION_NAME}.log"

    echo ""
    echo "=================================================="
    echo "🔄 当前执行版本: 【${VERSION_NAME}】"
    echo "=================================================="
    
    # [步骤A]：修改 go.mod 指向目标版本
    echo "⚙️ [A] 正在修改 go.mod 依赖指向: ${BADGER_PATH}"
    go mod edit -replace ${MODULE_NAME}=${BADGER_PATH}
    go mod tidy

    # [步骤B]：清理旧数据
    echo "🧹 [B] 正在清空旧数据: ${DATA_DIR}/*"
    rm -rf ${DATA_DIR}/*
    mkdir -p ${DATA_DIR} 
    echo "✅ 旧数据清理完毕。"

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
    echo "=== 【${VERSION_NAME}】 测试报告 ===" > ${LOG_FILE} 
    
    # --- Load 阶段 ---
    echo "📥 执行 Load 阶段..."
    echo ">>> --- Load Phase ---" >> ${LOG_FILE}
    ./bin/go-ycsb load badger -P workloads/workload_micro -P zzl_badger.properties >> ${LOG_FILE} 2>&1
    if [ $? -ne 0 ]; then
        echo "❌ Load 阶段报错，请查看 ${LOG_FILE}！"
        exit 1
    fi
    echo "✅ Load 阶段完成。"

    # --- Run 阶段 ---
    echo "🔥 执行 Run 阶段..."
    echo ">>> --- Run Phase ---" >> ${LOG_FILE}
    ./bin/go-ycsb run badger -P workloads/workload_micro -P zzl_badger.properties >> ${LOG_FILE} 2>&1
    
    # (保留你原来的注释逻辑：即使 Run 阶段报错也不一定退出，因为有的时候 YCSB 跑完会有非0退出码)
    # if [ $? -ne 0 ]; then
    #     echo "❌ Run 阶段报错！"
    #     exit 1
    # fi
    echo "✅ Run 阶段完成。"
    
    echo "🏁 【${VERSION_NAME}】 测试流程结束！"
}

# ================= 主执行流程 =================
START_TIME=$(date +%s)

# 第一轮：跑原版 Badger
run_single_test "Normal_Badger" ${NORMAL_BADGER_PATH}

# 第二轮：跑魔改版 (heatLSM) Badger
run_single_test "heatLSM_Badger" ${PRIZZL_BADGER_PATH}

# 善后：把开发环境切回你的魔改版，方便你接下来继续改代码
echo "🔧 测试完毕，正在将 go.mod 恢复指向你的开发版本..."
go mod edit -replace ${MODULE_NAME}=${PRIZZL_BADGER_PATH}
go mod tidy

END_TIME=$(date +%s)
echo "=================================================="
echo "🎉 所有压测流程执行完毕！总耗时: $((END_TIME - START_TIME)) 秒。"
echo "📊 请在当前目录下打开以下两个日志文件进行数据对比："
echo "   - result_Normal_Badger.log"
echo "   - result_heatLSM_Badger.log"
echo "=================================================="