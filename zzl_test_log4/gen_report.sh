#!/bin/bash
BASE="/home/hanjiang/DB-CODE/ZZL-go-ycsb/zzl_test_log4"
RESULT="$BASE/result.md"

# 解析 TOTAL/INSERT 行的函数
parse_ops_line() {
    local line="$1"
    echo "$line" | awk -F', ' '{
        ops=""; count=""; takes=""; avg=""; p50=""; p95=""; p99=""; p999=""
        for(i=1;i<=NF;i++) {
            if($i ~ /OPS:/) { gsub(/.*OPS: /,"",$i); ops=$i }
            if($i ~ /Count:/) { gsub(/.*Count: /,"",$i); count=$i }
            if($i ~ /Takes\(s\):/) { gsub(/.*Takes\(s\): /,"",$i); takes=$i }
            if($i ~ /Avg\(us\):/) { gsub(/.*Avg\(us\): /,"",$i); avg=$i }
            if($i ~ /50th\(us\):/) { gsub(/.*50th\(us\): /,"",$i); p50=$i }
            if($i ~ /95th\(us\):/) { gsub(/.*95th\(us\): /,"",$i); p95=$i }
            if($i ~ /^99\.9th\(us\):/) { gsub(/.*: /,"",$i); p999=$i }
            else if($i ~ /^99th\(us\):/) { gsub(/.*: /,"",$i); p99=$i }
        }
        printf "%s|%s|%s|%s|%s|%s|%s|%s", ops, count, takes, avg, p50, p95, p99, p999
    }'
}

# ===================== 写入报告 =====================
cat > "$RESULT" << 'EOF'
# ZZL Badger 测试结果对比分析

> 测试环境: 大机器, 用户 root
> Run 操作数: 1000万次 (operationcount=10000000)
> 负载: zipfian, read=50%, update=50%, fieldlength=1024, fieldcount=1
> Round1 = Load+Run; Round2/3 = 模板复用(Run only)
> `stabilization_time` 采用智能轮询等待 (非固定盲等)

---

EOF

# ================================================================
# 第一部分: t* 目录 - Load QPS, Run 延迟, 写放大
# ================================================================
echo "## 一、t* 目录: Load QPS / Run 延迟分布 / 写放大对比" >> "$RESULT"

for DIRNAME in t1000w t3000w t5000w t7000w t9000w; do
    DIR="$BASE/$DIRNAME"
    RC_NUM=$(echo "$DIRNAME" | grep -oP '\d+')
    RC_VAL="${RC_NUM}0000"
    echo "" >> "$RESULT"
    echo "---" >> "$RESULT"
    echo "" >> "$RESULT"
    echo "### 1.${DIRNAME#t}: $DIRNAME (Load=${RC_VAL}, Run=1000万)" >> "$RESULT"
    echo "" >> "$RESULT"

    # --- 1.x.1 Load QPS (Round1 only) ---
    echo "#### Load 阶段 QPS (Round1)" >> "$RESULT"
    echo "" >> "$RESULT"
    echo "| 版本 | 线程数 | Load QPS | Count | Avg(us) | P99(us) |" >> "$RESULT"
    echo "|------|--------|----------|-------|---------|---------|" >> "$RESULT"

    for T in 1 4 8 16 32 64; do
        for VER in "heatLSM_Badger_NL98M" "Normal_Badger"; do
            SHORT_VER=$(echo "$VER" | sed 's/heatLSM_Badger_NL98M/heatLSM/' | sed 's/Normal_Badger/Normal/')
            FILE="$DIR/t_result_${VER}_RC${RC_VAL}_T${T}_Round1.log"
            if [ -f "$FILE" ]; then
                LOAD_LINE=$(grep "INSERT.*OPS" "$FILE" | grep -v "Run Phase\|UPDATE" | tail -1)
                if [ -n "$LOAD_LINE" ]; then
                    PARSED=$(parse_ops_line "$LOAD_LINE")
                    OPS=$(echo "$PARSED" | cut -d'|' -f1)
                    COUNT=$(echo "$PARSED" | cut -d'|' -f2)
                    AVG=$(echo "$PARSED" | cut -d'|' -f4)
                    P99=$(echo "$PARSED" | cut -d'|' -f7)
                    echo "| $SHORT_VER | $T | $OPS | $COUNT | $AVG | $P99 |" >> "$RESULT"
                fi
            fi
        done
    done

    # --- 1.x.2 Run 阶段三轮对比 ---
    echo "" >> "$RESULT"
    echo "#### Run 阶段三轮延迟分布 & QPS & 写放大" >> "$RESULT"
    echo "" >> "$RESULT"

    for T in 1 4 8 16 32 64; do
        # 检查这个T是否有数据
        FIRST_FILE=$(ls "$DIR"/t_result_*_RC${RC_VAL}_T${T}_Round1.log 2>/dev/null | head -1)
        [ -z "$FIRST_FILE" ] && continue

        echo "##### 线程数 T=$T" >> "$RESULT"
        echo "" >> "$RESULT"
        echo "| 版本 | 轮次 | Run QPS | Avg(us) | P50(us) | P95(us) | P99(us) | P99.9(us) | 运行时间 | 写放大(WA) |" >> "$RESULT"
        echo "|------|------|---------|---------|---------|---------|---------|-----------|----------|-----------|" >> "$RESULT"

        for VER in "heatLSM_Badger_NL98M" "Normal_Badger"; do
            SHORT_VER=$(echo "$VER" | sed 's/heatLSM_Badger_NL98M/heatLSM/' | sed 's/Normal_Badger/Normal/')

            OPS_SUM=0; AVG_SUM=0; P50_SUM=0; P95_SUM=0; P99_SUM=0; P999_SUM=0; WA_SUM=0; ROUND_COUNT=0

            for R in 1 2 3; do
                FILE="$DIR/t_result_${VER}_RC${RC_VAL}_T${T}_Round${R}.log"
                if [ -f "$FILE" ]; then
                    TOTAL_LINE=$(grep "TOTAL.*OPS" "$FILE" | grep -v "Load Phase\|INSERT" | tail -1)
                    PARSED=$(parse_ops_line "$TOTAL_LINE")
                    OPS=$(echo "$PARSED" | cut -d'|' -f1)
                    AVG=$(echo "$PARSED" | cut -d'|' -f4)
                    P50=$(echo "$PARSED" | cut -d'|' -f5)
                    P95=$(echo "$PARSED" | cut -d'|' -f6)
                    P99=$(echo "$PARSED" | cut -d'|' -f7)
                    P999=$(echo "$PARSED" | cut -d'|' -f8)

                    # WA
                    WA_LINES=$(grep "System WA" "$FILE")
                    WA_COUNT=$(echo "$WA_LINES" | wc -l)
                    if [ "$R" -eq 1 ] && [ "$WA_COUNT" -ge 2 ]; then
                        WA=$(echo "$WA_LINES" | sed -n '2p' | grep -oP 'WA\):\s*\K[\d.]+')
                    elif [ "$WA_COUNT" -ge 1 ]; then
                        WA=$(echo "$WA_LINES" | sed -n '1p' | grep -oP 'WA\):\s*\K[\d.]+')
                    else
                        WA="N/A"
                    fi

                    RUN_TIME=$(grep "Run finished" "$FILE" | tail -1 | grep -oP 'takes\s*\K[\dms.]+')

                    echo "| $SHORT_VER | R$R | $OPS | $AVG | $P50 | $P95 | $P99 | $P999 | $RUN_TIME | $WA |" >> "$RESULT"

                    # 累加求均值
                    if [ "$OPS" != "" ] && [ "$WA" != "N/A" ]; then
                        OPS_SUM=$(awk "BEGIN {print $OPS_SUM + $OPS}")
                        AVG_SUM=$(awk "BEGIN {print $AVG_SUM + $AVG}")
                        P50_SUM=$(awk "BEGIN {print $P50_SUM + $P50}")
                        P95_SUM=$(awk "BEGIN {print $P95_SUM + $P95}")
                        P99_SUM=$(awk "BEGIN {print $P99_SUM + $P99}")
                        P999_SUM=$(awk "BEGIN {print $P999_SUM + $P999}")
                        WA_SUM=$(awk "BEGIN {print $WA_SUM + $WA}")
                        ROUND_COUNT=$((ROUND_COUNT + 1))
                    fi
                fi
            done

            # 三轮均值
            if [ "$ROUND_COUNT" -gt 0 ]; then
                AVG_OPS=$(awk "BEGIN {printf \"%.1f\", $OPS_SUM / $ROUND_COUNT}")
                AVG_AVG=$(awk "BEGIN {printf \"%.0f\", $AVG_SUM / $ROUND_COUNT}")
                AVG_P50=$(awk "BEGIN {printf \"%.0f\", $P50_SUM / $ROUND_COUNT}")
                AVG_P95=$(awk "BEGIN {printf \"%.0f\", $P95_SUM / $ROUND_COUNT}")
                AVG_P99=$(awk "BEGIN {printf \"%.0f\", $P99_SUM / $ROUND_COUNT}")
                AVG_P999=$(awk "BEGIN {printf \"%.0f\", $P999_SUM / $ROUND_COUNT}")
                AVG_WA=$(awk "BEGIN {printf \"%.2f\", $WA_SUM / $ROUND_COUNT}")

                echo "| **$SHORT_VER 均值** | | **$AVG_OPS** | **$AVG_AVG** | **$AVG_P50** | **$AVG_P95** | **$AVG_P99** | **$AVG_P999** | | **$AVG_WA** |" >> "$RESULT"

                # 计算过程
                echo "" >> "$RESULT"
                echo "<details><summary>📐 ${SHORT_VER} T${T} 均值计算过程</summary>" >> "$RESULT"
                echo "" >> "$RESULT"
                echo '```' >> "$RESULT"
                echo "OPS 均值    = ($(awk "BEGIN {printf \"%.1f\", $OPS_SUM}") / $ROUND_COUNT) = $AVG_OPS" >> "$RESULT"
                echo "Avg 均值    = ($(awk "BEGIN {printf \"%.0f\", $AVG_SUM}") / $ROUND_COUNT) = $AVG_AVG us" >> "$RESULT"
                echo "P50 均值    = ($(awk "BEGIN {printf \"%.0f\", $P50_SUM}") / $ROUND_COUNT) = $AVG_P50 us" >> "$RESULT"
                echo "P95 均值    = ($(awk "BEGIN {printf \"%.0f\", $P95_SUM}") / $ROUND_COUNT) = $AVG_P95 us" >> "$RESULT"
                echo "P99 均值    = ($(awk "BEGIN {printf \"%.0f\", $P99_SUM}") / $ROUND_COUNT) = $AVG_P99 us" >> "$RESULT"
                echo "P999 均值   = ($(awk "BEGIN {printf \"%.0f\", $P999_SUM}") / $ROUND_COUNT) = $AVG_P999 us" >> "$RESULT"
                echo "WA 均值     = ($WA_SUM / $ROUND_COUNT) = $AVG_WA x" >> "$RESULT"
                echo '```' >> "$RESULT"
                echo "" >> "$RESULT"
                echo "</details>" >> "$RESULT"
                echo "" >> "$RESULT"
            fi
        done
    done
done

# ================================================================
# 第二部分: tv* 目录 - GC 分析
# ================================================================
echo "" >> "$RESULT"
echo "---" >> "$RESULT"
echo "" >> "$RESULT"
echo "## 二、tv* 目录: GC 回收效率分析" >> "$RESULT"
echo "" >> "$RESULT"
echo "> tv 表示 Vlog 回收测试，Load=Run 数据量相同" >> "$RESULT"
echo "> fieldlength=2048, value_threshold=1024 (分裂阈值), 触发 Vlog 写入" >> "$RESULT"
echo "> 线程数: T=8" >> "$RESULT"
echo "" >> "$RESULT"

for DIRNAME in tv3000w tv5000w; do
    DIR="$BASE/$DIRNAME"
    RC_NUM=$(echo "$DIRNAME" | grep -oP '\d+')
    RC_VAL="${RC_NUM}0000"
    echo "### 2.${DIRNAME#tv}: $DIRNAME (Load=${RC_VAL}, Run=${RC_VAL})" >> "$RESULT"
    echo "" >> "$RESULT"
    echo "| 版本 | 轮次 | GC耗时(ms) | GC额外物理写(MB) | GC回收空间(MB) |" >> "$RESULT"
    echo "|------|------|-----------|-----------------|---------------|" >> "$RESULT"

    PHYS_SUM_heat=0; RECLAIM_SUM_heat=0; COUNT_heat=0
    PHYS_SUM_norm=0; RECLAIM_SUM_norm=0; COUNT_norm=0

    for R in 1 2 3; do
        for VER in "heatLSM_Badger_NL98M" "Normal_Badger"; do
            SHORT_VER=$(echo "$VER" | sed 's/heatLSM_Badger_NL98M/heatLSM/' | sed 's/Normal_Badger/Normal/')
            FILE="$DIR/t_result_${VER}_V_RC${RC_VAL}_T8_Round${R}.log"
            if [ -f "$FILE" ]; then
                GC_LINE=$(grep "GC_SUMMARY" "$FILE")
                TOTAL_MS=$(echo "$GC_LINE" | grep -oP 'total_time_ms=\K[\d]+')
                PHYS_MB=$(echo "$GC_LINE" | grep -oP 'phys_write_mb=\K[\d.]+')
                RECLAIM_MB=$(echo "$GC_LINE" | grep -oP 'space_reclaimed_mb=\K[\d.]+')
                echo "| $SHORT_VER | R$R | $TOTAL_MS | $PHYS_MB | $RECLAIM_MB |" >> "$RESULT"

                if [ "$SHORT_VER" = "heatLSM" ]; then
                    PHYS_SUM_heat=$(awk "BEGIN {print $PHYS_SUM_heat + $PHYS_MB}")
                    RECLAIM_SUM_heat=$(awk "BEGIN {print $RECLAIM_SUM_heat + $RECLAIM_MB}")
                    COUNT_heat=$((COUNT_heat + 1))
                else
                    PHYS_SUM_norm=$(awk "BEGIN {print $PHYS_SUM_norm + $PHYS_MB}")
                    RECLAIM_SUM_norm=$(awk "BEGIN {print $RECLAIM_SUM_norm + $RECLAIM_MB}")
                    COUNT_norm=$((COUNT_norm + 1))
                fi
            fi
        done
    done

    # 均值
    AVG_PHYS_heat=$(awk "BEGIN {printf \"%.2f\", $PHYS_SUM_heat / $COUNT_heat}")
    AVG_RECLAIM_heat=$(awk "BEGIN {printf \"%.2f\", $RECLAIM_SUM_heat / $COUNT_heat}")
    AVG_PHYS_norm=$(awk "BEGIN {printf \"%.2f\", $PHYS_SUM_norm / $COUNT_norm}")
    AVG_RECLAIM_norm=$(awk "BEGIN {printf \"%.2f\", $RECLAIM_SUM_norm / $COUNT_norm}")

    echo "| **heatLSM 均值** | | | **$AVG_PHYS_heat** | **$AVG_RECLAIM_heat** |" >> "$RESULT"
    echo "| **Normal 均值** | | | **$AVG_PHYS_norm** | **$AVG_RECLAIM_norm** |" >> "$RESULT"
    echo "" >> "$RESULT"

    # 计算过程
    echo "<details><summary>📐 $DIRNAME GC 均值计算过程</summary>" >> "$RESULT"
    echo "" >> "$RESULT"
    echo '```' >> "$RESULT"
    echo "heatLSM GC额外物理写均值 = ($PHYS_SUM_heat / $COUNT_heat) = $AVG_PHYS_heat MB" >> "$RESULT"
    echo "heatLSM GC回收空间均值     = ($RECLAIM_SUM_heat / $COUNT_heat) = $AVG_RECLAIM_heat MB" >> "$RESULT"
    echo "Normal  GC额外物理写均值 = ($PHYS_SUM_norm / $COUNT_norm) = $AVG_PHYS_norm MB" >> "$RESULT"
    echo "Normal  GC回收空间均值     = ($RECLAIM_SUM_norm / $COUNT_norm) = $AVG_RECLAIM_norm MB" >> "$RESULT"
    echo "" >> "$RESULT"
    echo "# GC写放大效率 = 回收空间 / 额外物理写" >> "$RESULT"
    HEAT_EFF=$(awk "BEGIN {if($AVG_PHYS_heat > 0) printf \"%.2f\", $AVG_RECLAIM_heat / $AVG_PHYS_heat; else print \"N/A\"}")
    NORM_EFF=$(awk "BEGIN {if($AVG_PHYS_norm > 0) printf \"%.2f\", $AVG_RECLAIM_norm / $AVG_PHYS_norm; else print \"N/A\"}")
    echo "heatLSM GC效率 = $AVG_RECLAIM_heat / $AVG_PHYS_heat = ${HEAT_EFF}x (每MB物理写回收 ${HEAT_EFF}MB)" >> "$RESULT"
    echo "Normal  GC效率 = $AVG_RECLAIM_norm / $AVG_PHYS_norm = ${NORM_EFF}x (每MB物理写回收 ${NORM_EFF}MB)" >> "$RESULT"
    echo '```' >> "$RESULT"
    echo "" >> "$RESULT"
    echo "</details>" >> "$RESULT"
    echo "" >> "$RESULT"
done

echo "" >> "$RESULT"
echo "---" >> "$RESULT"
echo "" >> "$RESULT"
echo "> 报告生成时间: $(date)" >> "$RESULT"

echo "Report generated: $RESULT"
