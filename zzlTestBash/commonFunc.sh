#!/bin/bash
source ./dgraphGroupVars.sh # 引入集群节点配置文件
remote_kill_by_port() {
    # 检查参数是否完整
    if [ $# -ne 4 ]; then
        echo "错误：参数不足！用法: remote_kill_by_port <ip> <user> <password> <port>"
        return 1
    fi

    local remote_ip="$1"
    local remote_user="$2"
    local remote_pass="$3"
    local target_port="$4"

    echo "开始关闭远程服务器 $remote_ip 的 $target_port 端口进程..."

    # 核心命令：通过 ssh 远程执行端口查杀
    # 逻辑说明：
    # 1. 远程执行 lsof 查找端口对应的PID（需 root 权限，用 sudo）
    # 2. 过滤掉表头，提取PID并强制终止（kill -9）
    # 3. 重定向输出到/dev/null避免干扰，仅保留错误信息
    sshpass -p "$remote_pass" ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 \
        "${remote_user}@${remote_ip}" \
        "sudo lsof -i :${target_port} | grep -v 'PID' | awk '{print \$2}' | xargs -r sudo kill -9" 2>/tmp/ssh_error.log

    # 检查执行结果
    local exit_code=$?
    if [ $exit_code -eq 0 ]; then
        # 验证端口是否已关闭（二次检查）
        check_result=$(sshpass -p "$remote_pass" ssh -o StrictHostKeyChecking=no \
            "${remote_user}@${remote_ip}" "sudo lsof -i :${target_port} | grep -v 'PID' | wc -l")
        
        if [ "$check_result" -eq 0 ]; then
            echo "成功：远程 $remote_ip 的 $target_port 端口进程已关闭（或未占用）"
            return 0
        else
            echo "警告：远程 $remote_ip 的 $target_port 端口仍有进程残留，请手动检查"
            return 0  # 视为成功，仅警告
        fi
    else
        echo "错误：操作失败！原因：$(cat /tmp/ssh_error.log | tail -n 1)"
        return 1
    fi
}
# 定义 sshpass 通用函数
# 参数1：目标主机IP（必填）
# 参数2：是否启用 -f 标识符（可选，传 "f" 则添加 -f，默认不启用）
# 参数3：远程执行的命令（必填，需用单引号包裹以保留特殊字符）
sshpass_exec() {
    local remote_ip="$1"                  # 目标主机IP（必填）
    local use_ssh_f="${2:-false}"         # 是否启用 -f（默认不启用）
    local remote_cmd="$3"                 # 远程执行的命令（必填）

    # 检查必填参数
    if [ -z "$remote_ip" ] || [ -z "$remote_cmd" ]; then
        echo "错误：目标主机IP和远程命令为必填参数！"
        echo "$remote_ip"
        echo "$remote_cmd"
        return 1
    fi

    # 构建 ssh 选项（是否添加 -f）
    local ssh_options="-o StrictHostKeyChecking=no"
    if [ "$use_ssh_f" = "f" ]; then
        ssh_options="$ssh_options -f"  # 启用后台运行
    fi

    # 执行 sshpass 命令
    echo "正在通过 sshpass 连接 $remote_user@$remote_ip，执行命令：$remote_cmd"
    sshpass -p "$remote_password" ssh $ssh_options "${remote_user}@${remote_ip}" "$remote_cmd"
    # sshpass -p "314hanjiang" ssh -o StrictHostKeyChecking=no root@192.168.137.102 "cd /"

    # 检查执行结果
    local exit_code=$?
    if [ $exit_code -eq 0 ]; then
        echo "✅ $remote_user@$remote_ip 命令执行成功"
    else
        echo "❌ $remote_user@$remote_ip 命令执行失败（退出码：$exit_code）"
    fi
    return $exit_code
}