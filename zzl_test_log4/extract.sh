#!/bin/bash
# 批量提取测试日志关键指标
BASE="/home/hanjiang/DB-CODE/ZZL-go-ycsb/zzl_test_log4"
OUTPUT="$BASE/extracted_data.txt"

echo "== 数据提取 $(date) ==" > "$OUTPUT"

for DIR in "$BASE"/t*/; do
    DIRNAME=$(basename "$DIR")
    echo "--- 处理目录: $DIRNAME ---" >> "$OUTPUT"

    for FILE in "$DIR"*.log; do
        FNAME=$(basename "$FILE")

        # 解析文件名: t_result_{VERSION}_RC{RC}_T{THREADS}_Round{ROUND}.log
        if [[ "$FNAME" =~ t_result_(.+)_RC([0-9]+)_T([0-9]+)_Round([0-9]+)\.log ]]; then
            VERSION="${BASH_REMATCH[1]}"
            RC="${BASH_REMATCH[2]}"
            THREADS="${BASH_REMATCH[3]}"
            ROUND="${BASH_REMATCH[4]}"
        else
            continue
        fi

        echo "  FILE=$FNAME VERSION=$VERSION RC=$RC T=$THREADS R=$ROUND" >> "$OUTPUT"

        # --- Load QPS (仅Round1) ---
        if [ "$ROUND" -eq 1 ]; then
            LOAD_INSERT=$(grep "INSERT.*OPS" "$FILE" | grep -v "Load Phase\|Run Phase" | tail -1)
            if [ -n "$LOAD_INSERT" ]; then
                LOAD_OPS=$(echo "$LOAD_INSERT" | grep -oP 'OPS:\s*\K[\d.]+')
                LOAD_COUNT=$(echo "$LOAD_INSERT" | grep -oP 'Count:\s*\K[\d]+')
                LOAD_AVG=$(echo "$LOAD_INSERT" | grep -oP 'Avg\(us\):\s*\K[\d]+')
                LOAD_P99=$(echo "$LOAD_INSERT" | grep -oP '99th\(us\):\s*\K[\d]+')
                echo "    LOAD_QPS=$LOAD_OPS COUNT=$LOAD_COUNT AvgUs=$LOAD_AVG P99Us=$LOAD_P99" >> "$OUTPUT"
            fi
        fi

        # --- Run TOTAL (最后一个在Run Phase中的TOTAL行) ---
        # 取整个文件最后一个 TOTAL.*OPS 行
        RUN_TOTAL=$(grep "TOTAL.*OPS" "$FILE" | grep -v "Load Phase\|INSERT" | tail -1)
        if [ -n "$RUN_TOTAL" ]; then
            RUN_OPS=$(echo "$RUN_TOTAL" | grep -oP 'OPS:\s*\K[\d.]+')
            RUN_COUNT=$(echo "$RUN_TOTAL" | grep -oP 'Count:\s*\K[\d]+')
            RUN_AVG=$(echo "$RUN_TOTAL" | grep -oP 'Avg\(us\):\s*\K[\d]+')
            RUN_P50=$(echo "$RUN_TOTAL" | grep -oP '50th\(us\):\s*\K[\d]+')
            RUN_P95=$(echo "$RUN_TOTAL" | grep -oP '95th\(us\):\s*\K[\d]+')
            RUN_P99=$(echo "$RUN_TOTAL" | grep -oP '99th\(us\):\s*\K[\d]+')
            RUN_P999=$(echo "$RUN_TOTAL" | grep -oP '99\.9th\(us\):\s*\K[\d]+')
            RUN_TAKES=$(echo "$RUN_TOTAL" | grep -oP 'Takes\(s\):\s*\K[\d.]+')
            echo "    RUN_OPS=$RUN_OPS COUNT=$RUN_COUNT TakesS=$RUN_TAKES AvgUs=$RUN_AVG P50Us=$RUN_P50 P95Us=$RUN_P95 P99Us=$RUN_P99 P999Us=$RUN_P999" >> "$OUTPUT"
        fi

        # --- Run finished time ---
        RUN_FINISHED=$(grep "Run finished" "$FILE" | tail -1)
        if [ -n "$RUN_FINISHED" ]; then
            echo "    RUN_FINISHED=$RUN_FINISHED" >> "$OUTPUT"
        fi

        # --- Write Amplification (Run phase) ---
        # Round1: Load WA + Run WA + GC WA → 取第2个
        # Round2/3: Run WA + GC WA → 取第1个
        WA_LINES=$(grep "System WA" "$FILE")
        WA_COUNT=$(echo "$WA_LINES" | wc -l)
        if [ "$ROUND" -eq 1 ] && [ "$WA_COUNT" -ge 2 ]; then
            RUN_WA=$(echo "$WA_LINES" | sed -n '2p' | grep -oP 'WA\):\s*\K[\d.]+')
        elif [ "$WA_COUNT" -ge 1 ]; then
            RUN_WA=$(echo "$WA_LINES" | sed -n '1p' | grep -oP 'WA\):\s*\K[\d.]+')
        fi
        if [ -n "$RUN_WA" ]; then
            echo "    RUN_WA=$RUN_WA" >> "$OUTPUT"
        fi

        # --- PHYSICAL_IO_RESULT ---
        PHYSICAL=$(grep "PHYSICAL_IO_RESULT" "$FILE")
        if [ -n "$PHYSICAL" ]; then
            echo "    PHYSICAL_IO=$PHYSICAL" >> "$OUTPUT"
        fi

        # --- GC Summary ---
        GC_SUMMARY=$(grep "GC_SUMMARY" "$FILE")
        if [ -n "$GC_SUMMARY" ]; then
            echo "    GC_SUMMARY=$GC_SUMMARY" >> "$OUTPUT"
        fi

        echo "" >> "$OUTPUT"
    done
done

echo "=== 提取完成 ===" >> "$OUTPUT"
echo "Output written to $OUTPUT"
