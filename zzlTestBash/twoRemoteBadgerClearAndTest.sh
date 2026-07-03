#!/bin/bash
# NOTE: 本脚本自动切换原版与魔改版BadgerDB，进行双版本本地编译，并将产物传送到目标主机执行远程压测

# ================= 远程主机配置区 =================
target_ips=("192.168.137.105" "172.19.71.5")  # 目标主机 IP 列表
# remote_user="root"              # 目标主机的用户名
# remote_password="314hanjiang"   # 目标主机的 SSH 登录密码（明文）
remote_user="zlzhao"              # 目标主机的用户名
remote_password="qwertyuiop!@#"   # 目标主机的 SSH 登录密码（明文）
REMOTE_IP="${target_ips[1]}"    # 这里取第一个IP作为压测目标机

# 远程机器的工作目录和数据目录
# REMOTE_WORKSPACE="/zzlBadger"
REMOTE_WORKSPACE="/pfm/disks/core/zzlBadger"
REMOTE_DATA_DIR="${REMOTE_WORKSPACE}/zzl_badger_data"

# ================= 本地环境配置区 =================
MODULE_NAME="github.com/dgraph-io/badger/v4"
NORMAL_BADGER_PATH="/home/hanjiang/DB-CODE/nomalBadger"
PRIZZL_BADGER_PATH="/home/hanjiang/DB-CODE/priZzlBadger"

# ================= 基础通用函数 =================
sshpass_exec() {
    local r_ip="$1"
    local use_ssh_f="${2:-false}"
    local remote_cmd="$3"

    if [ -z "$r_ip" ] || [ -z "$remote_cmd" ]; then
        echo "错误：目标主机IP和远程命令为必填参数！"
        return 1
    fi

    local ssh_options="-o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10"
    if [ "$use_ssh_f" = "f" ]; then
        ssh_options="$ssh_options -f"
    fi

    echo "正在通过 sshpass 连接 $remote_user@$r_ip，执行命令：$remote_cmd"
    sshpass -p "$remote_password" ssh $ssh_options "${remote_user}@${r_ip}" "$remote_cmd"
    
    local exit_code=$?
    if [ $exit_code -eq 0 ]; then
        echo "✅ $remote_user@$r_ip 命令执行成功"
    else
        echo "❌ $remote_user@$r_ip 命令执行失败（退出码：$exit_code）"
    fi
    return $exit_code
}

# ================= 0. 参数解析 & 远程初始化 =================
ROUNDS=${1:-1}

if ! [[ "$ROUNDS" =~ ^[1-9][0-9]*$ ]]; then
    echo "❌ 错误: 轮次参数必须是正整数！例如: ./run_test.sh 3"
    exit 1
fi

cd "$(dirname "$0")/.." || { echo "切换到项目根目录失败"; exit 1; }

echo "=================================================="
echo "🚀 开始一键【本地编译 -> 远程压测】双版本对照流程..."
echo "🎯 计划执行轮次: ${ROUNDS} 轮"
echo "🌐 目标测试主机: ${REMOTE_IP}"
echo "=================================================="

# 初始化远程主机的工作目录
echo "📁 正在初始化远程主机工作目录..."
sshpass_exec "$REMOTE_IP" "false" "mkdir -p ${REMOTE_WORKSPACE}/bin ${REMOTE_WORKSPACE}/workloads"

# ================= 核心测试逻辑函数 =================
run_single_test() {
    local VERSION_NAME=$1
    local BADGER_PATH=$2
    local ROUND=$3
    local THREADS=$4
    local RC=$5

    local LOG_FILE="t_result_${VERSION_NAME}_RC${RC}_T${THREADS}_Round${ROUND}.log"

    echo ""
    echo "--------------------------------------------------"
    echo "🔄 版本: 【${VERSION_NAME}】 | 数据量: 【${RC}】 | 线程数: 【${THREADS}】 | 轮次: 【第 ${ROUND} 轮】"
    echo "--------------------------------------------------"

    # [步骤A]：修改本地 go.mod 指向目标版本
    echo "⚙️ [A] 正在修改本地 go.mod 指向: ${BADGER_PATH}"
    go mod edit -replace ${MODULE_NAME}=${BADGER_PATH}
    go mod tidy

    # [步骤B]：本地重新编译
    echo "🔨 [B] 正在本地重新编译 go-ycsb..."
    make build
    if [ $? -ne 0 ]; then
        echo "❌ 【${VERSION_NAME}】 本地编译失败，停止测试！请检查代码！"
        exit 1
    fi
    echo "✅ 本地编译成功。"

    # [步骤C]：推送可执行文件和配置到远程主机
    echo "🚀 [C] 正在将压测程序推送到远程主机 ${REMOTE_IP} ..."
    sshpass -p "$remote_password" scp -o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10 ./bin/go-ycsb ${remote_user}@${REMOTE_IP}:${REMOTE_WORKSPACE}/bin/
    sshpass -p "$remote_password" scp -o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10 ./zzl_badger.properties ${remote_user}@${REMOTE_IP}:${REMOTE_WORKSPACE}/
    # 覆盖推 workloads 文件夹，以防负载有变动
    sshpass -p "$remote_password" scp -o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10 -r ./workloads/* ${remote_user}@${REMOTE_IP}:${REMOTE_WORKSPACE}/workloads/
    echo "✅ 文件推送完毕。"

    # [步骤D]：清理远程机器的旧数据
    echo "🧹 [D] 正在清空远程主机的旧数据: ${REMOTE_DATA_DIR}/*"
    sshpass_exec "$REMOTE_IP" "false" "rm -rf ${REMOTE_DATA_DIR}/* && mkdir -p ${REMOTE_DATA_DIR}"

    # 下面这个是为了在每轮中间加上冷却时间，小机器不知道什么原因，可能是过热也可能是SSD要整理数据，如果连续运行会导致一轮比一轮的速度慢，导致测试结果失真
    # echo "⚡ 正在触发底层 SSD 硬件级空间回收 (fstrim)..."
    # sshpass_exec "$REMOTE_IP" "false" "fstrim -v /"
    sleep 10
    # echo "⏳ 冷却 30 秒，等待 SSD 恢复至满血写入性能..."
    # sleep 30

    echo "✅ 远程旧数据清理完毕。"
    
    # ⚠️ 远程清空操作系统页缓存 (因为目标机器是 root 用户，无需 sudo)
    # echo "🧼 正在清空远程操作系统页缓存 ..."
    # sshpass_exec "$REMOTE_IP" "false" "sync; echo 3 > /proc/sys/vm/drop_caches"
    # echo "✅ 远程环境净化完毕。"

    # [步骤E]：开始远程压测并实时拉取日志保存到本地
    echo "🏃 [E] 开始远程压测！日志将实时写入本地 -> ${LOG_FILE}"
    echo "=== 【${VERSION_NAME}】 Threads: ${THREADS}, Round: ${ROUND} 测试报告 ===" > ${LOG_FILE} 
    
    # --- Load 阶段 ---
    echo "📥 执行远程 Load 阶段..."
    echo ">>> --- Load Phase ---" >> ${LOG_FILE}
    # 直接利用 ssh 命令将远程标准输出和错误重定向追加到本地 LOG_FILE 中
    sshpass -p "$remote_password" ssh -o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10 ${remote_user}@${REMOTE_IP} \
        "cd ${REMOTE_WORKSPACE} && ./bin/go-ycsb load badger -P workloads/workloada -P zzl_badger.properties -p threadcount=${THREADS} -p recordcount=${RC} -p stabilization_time=${STABILIZE_TIMES[0]}" >> ${LOG_FILE} 2>&1
    if [ $? -ne 0 ]; then
        echo "❌ Load 阶段报错，请查看本地 ${LOG_FILE}！"
        exit 1
    fi
    echo "✅ 远程 Load 阶段完成。"

    # 再次清空远程缓存
    # echo "🧼 正在清空远程操作系统页缓存 ..."
    # sshpass_exec "$REMOTE_IP" "false" "sync; echo 3 > /proc/sys/vm/drop_caches"

    # --- Run 阶段 ---
    echo "🔥 执行远程 Run 阶段..."
    echo ">>> --- Run Phase ---" >> ${LOG_FILE}
    sshpass -p "$remote_password" ssh -o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10 ${remote_user}@${REMOTE_IP} \
        "cd ${REMOTE_WORKSPACE} && ./bin/go-ycsb run badger -P workloads/workloada -P zzl_badger.properties -p threadcount=${THREADS} -p recordcount=${RC} -p operationcount=${OPCOUNT} -p stabilization_time=${STABILIZE_TIMES[1]}" >> ${LOG_FILE} 2>&1

    echo "✅ 远程 Run 阶段完成。"

    # --- GC 阶段 ---
    echo "🧹 [GC] 执行远程 GC 阶段 (3轮, 每轮回收垃圾率最高的Vlog文件)..."
    echo ">>> --- GC Phase ---" >> ${LOG_FILE}
    sshpass -p "$remote_password" ssh -o StrictHostKeyChecking=no -o ServerAliveInterval=60 -o ServerAliveCountMax=10 ${remote_user}@${REMOTE_IP} \
        "cd ${REMOTE_WORKSPACE} && ./bin/go-ycsb gc badger -P zzl_badger.properties -p gc.count=3 -p gc.discard_ratio=0.1 -p stabilization_time=10" >> ${LOG_FILE} 2>&1
    echo "✅ 远程 GC 阶段完成。"

    echo "🏁 【${VERSION_NAME}】 T${THREADS} 第 ${ROUND} 轮测试流程结束！"
}
 
# ================= 主执行流程 =================
START_TIME=$(date +%s)
RECORD_COUNTS=(10000000 30000000 50000000 70000000 90000000)
THREAD_COUNTS=(1 4 16 32 64)
OPCOUNT=10000000
# 稳定等待时间数组: [0]=Load阶段等待秒数, [1]=Run阶段等待秒数
STABILIZE_TIMES=(500 500)

echo "📊 计划测试的数据量列表: ${RECORD_COUNTS[*]}"
echo "📊 计划测试的线程数列表: ${THREAD_COUNTS[*]}"
echo "📊 操作数: ${OPCOUNT}"
echo "📊 稳定等待: Load=${STABILIZE_TIMES[0]}s, Run=${STABILIZE_TIMES[1]}s"

for RC in "${RECORD_COUNTS[@]}"; do
    echo ""
    echo "██████████████████████████████████████████████████"
    echo "🔥 开始针对数据量 【${RC}】 的全面压测"
    echo "██████████████████████████████████████████████████"

    for TC in "${THREAD_COUNTS[@]}"; do
        echo ""
        echo "=================================================="
        echo "🔥 数据量: 【${RC}】 | 线程数: 【${TC}】"
        echo "=================================================="

        for (( i=1; i<=ROUNDS; i++ )); do
            echo ""
            echo "🔔 [数据量: ${RC}] [线程数: ${TC}] 正在开始第 【${i} / ${ROUNDS}】 轮对照测试"

            # 跑魔改版 (heatLSM) Badger
            run_single_test "heatLSM_Badger_NL98M" ${PRIZZL_BADGER_PATH} $i $TC $RC

            # 跑原版 Badger
            run_single_test "Normal_Badger" ${NORMAL_BADGER_PATH} $i $TC $RC
        done
    done
done

# 善后：把开发环境切回魔改版
echo ""
echo "🔧 测试完毕，正在将本地 go.mod 恢复指向开发版本..."
go mod edit -replace ${MODULE_NAME}=${PRIZZL_BADGER_PATH}
go mod tidy

END_TIME=$(date +%s)
echo "=================================================="
echo "🎉 所有远程压测流程执行完毕！总耗时: $((END_TIME - START_TIME)) 秒。"
echo "📊 本地已生成日志文件: result_<版本>_T<线程数>_Round<轮次>.log"
echo "=================================================="