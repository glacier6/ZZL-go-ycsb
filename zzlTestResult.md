TODO:给我各测试负载的组成



下面是启动命令
./bin/go-ycsb load badger -P workloads/workloada -P zzl_badger.properties \
    -p recordcount=1000000 \
    -p threadcount=8

./bin/go-ycsb run badger -P workloads/workloada -P zzl_badger.properties \
    -p recordcount=10000 \
    -p operationcount=10000 \
    -p requestdistribution=zipfian \
    -p threadcount=16



下面是逐行解析A负载测试结果的样例
Run finished, takes 151.030789ms
整个测试运行只持续了 0.15 秒。

READ   - Takes(s): 0.2, Count: 4909, OPS: 32484.8, Avg(us): 227, Min(us): 3, Max(us): 10095, 50th(us): 212, 90th(us): 317, 95th(us): 389, 99th(us): 601, 99.9th(us): 8567, 99.99th(us): 10095
Count: 4909：执行了 4909 次读操作（接近总操作的一半，符合 Workload A 的 50/50 比例）。
OPS: 32484.8：每秒读取约 3.2 万次。
Avg(us): 227：平均读取延迟为 227 微秒 (0.22 毫秒)。
99th(us): 601：99% 的请求在 0.6 毫秒内完成。

TOTAL  - Takes(s): 0.2, Count: 9923, OPS: 65692.9, Avg(us): 237, Min(us): 3, Max(us): 10095, 50th(us): 220, 90th(us): 334, 95th(us): 415, 99th(us): 648, 99.9th(us): 8059, 99.99th(us): 8615
OPS: 65692.9：读+写总吞吐量约为 6.5 万 OPS。

UPDATE - Takes(s): 0.2, Count: 5014, OPS: 33183.0, Avg(us): 246, Min(us): 7, Max(us): 8615, 50th(us): 227, 90th(us): 354, 95th(us): 441, 99th(us): 690, 99.9th(us): 5343, 99.99th(us): 8567
OPS: 33183.0：每秒更新约 3.3 万次。
Avg(us): 246：平均写入延迟 246 微秒。

UPDATE_ERROR - Takes(s): 0.1, Count: 77, OPS: 538.6, Avg(us): 304, Min(us): 69, Max(us): 4643, 50th(us): 228, 90th(us): 388, 95th(us): 464, 99th(us): 526, 99.9th(us): 4643, 99.99th(us): 4643
Count：77次更新错误

badger 2025/12/24 16:06:21 INFO: Lifetime L0 stalled for: 0s
badger 2025/12/24 16:06:21 INFO: 
Level 0 [ ]: NumTables: 03. Size: 120 MiB of 0 B. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 64 MiB
Level 1 [ ]: NumTables: 00. Size: 0 B of 10 MiB. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 2.0 MiB
Level 2 [ ]: NumTables: 00. Size: 0 B of 10 MiB. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 2.0 MiB
Level 3 [ ]: NumTables: 00. Size: 0 B of 10 MiB. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 2.0 MiB
Level 4 [ ]: NumTables: 00. Size: 0 B of 10 MiB. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 2.0 MiB
Level 5 [B]: NumTables: 21. Size: 82 MiB of 84 MiB. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 4.0 MiB （基准层）
Level 6 [ ]: NumTables: 175. Size: 843 MiB of 843 MiB. Score: 0.00->0.00 StaleData: 0 B Target FileSize: 8.0 MiB
Level Done